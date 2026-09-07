package service

import (
	"encoding/json"
	"fmt"
	"path"
	"regexp"
	"strconv"
	"strings"
)

var workbenchSRSRE = regexp.MustCompile(`<!--[ \t]*srs:[ \t]*(\{.*?\})[ \t]*-->`)

func workbenchCardState(raw string) (InterviewCard, error) {
	var state struct {
		Level               string `json:"level"`
		Interval            int    `json:"interval"`
		Due                 string `json:"due"`
		Reps                int    `json:"reps"`
		Failures            *int   `json:"failures"`
		ConsecutiveFailures *int   `json:"consecutiveFailures"`
		LastReviewed        string `json:"lastReviewed"`
		LastReviewedSnake   string `json:"last_reviewed"`
		Weak                bool   `json:"weak"`
	}
	if err := json.Unmarshal([]byte(raw), &state); err != nil {
		return InterviewCard{}, err
	}
	if state.Due != "" {
		if _, err := workbenchDate(state.Due); err != nil {
			return InterviewCard{}, err
		}
	}
	if state.Level == "完全不会" {
		state.Level = "不会"
	}
	if state.LastReviewed == "" {
		state.LastReviewed = state.LastReviewedSnake
	}
	failures := 0
	if state.Failures != nil {
		failures = max(0, *state.Failures)
	} else if state.ConsecutiveFailures != nil {
		failures = max(0, *state.ConsecutiveFailures)
	} else if state.Level == "不会" {
		failures = 1
	}
	return InterviewCard{Level: state.Level, Interval: max(0, state.Interval), Due: state.Due,
		Reps: max(0, state.Reps), Failures: failures, LastReviewed: state.LastReviewed, Weak: state.Weak || failures >= 2}, nil
}

func parseWorkbenchCards(file workbenchFile) ([]InterviewCard, []string) {
	cards := []InterviewCard{}
	warnings := []string{}
	question, questionLevel := "", 0
	for i, line := range file.lines {
		if !file.visible[i] {
			continue
		}
		if heading := workbenchHeadingRE.FindStringSubmatch(line.text); heading != nil {
			question, questionLevel = strings.TrimSpace(heading[2]), len(heading[1])
		}
		match := workbenchSRSRE.FindStringSubmatch(line.text)
		if match == nil {
			continue
		}
		card, err := workbenchCardState(match[1])
		if err != nil || question == "" {
			warnings = append(warnings, fmt.Sprintf("%s:%d 的面试卡片格式无效", file.path, i+1))
			continue
		}
		end := len(file.lines)
		for j := i + 1; j < len(file.lines); j++ {
			if file.visible[j] {
				if heading := workbenchHeadingRE.FindStringSubmatch(file.lines[j].text); heading != nil && len(heading[1]) <= questionLevel {
					end = j
					break
				}
			}
		}
		card.ID, card.FilePath, card.LineIndex = file.path+":"+strconv.Itoa(i), file.path, i
		card.Comment, card.Question = match[0], question
		card.Answer = strings.TrimSpace(joinWorkbenchLines(file.lines[i+1 : end]))
		cards = append(cards, card)
	}
	return cards, warnings
}

// ReviewInterviewCard updates only known JSON values inside the original comment.
func (s *TodoService) ReviewInterviewCard(workspacePath, filePath string, lineIndex int, expectedComment, level, date string) error {
	day, err := workbenchDate(date)
	if err != nil {
		return err
	}
	intervals := map[string]int{"掌握": 7, "模糊": 3, "不会": 1}
	interval, ok := intervals[level]
	if !ok {
		return fmt.Errorf("未知复习评价：%s", level)
	}
	s.workbenchMu.Lock()
	defer s.workbenchMu.Unlock()
	change, err := readWorkbenchChange(workspacePath, filePath)
	if err != nil {
		return err
	}
	if !change.existed || (strings.ToLower(path.Ext(change.relative)) != ".md" && strings.ToLower(path.Ext(change.relative)) != ".markdown") {
		return fmt.Errorf("找不到面试卡片的 Markdown 文件")
	}
	file := newWorkbenchFile(change.relative, change.before, "")
	if lineIndex < 0 || lineIndex >= len(file.lines) || !file.visible[lineIndex] {
		return fmt.Errorf("面试卡片已变化，请刷新后重试")
	}
	line := file.lines[lineIndex].text
	match := workbenchSRSRE.FindStringSubmatchIndex(line)
	if match == nil || line[match[0]:match[1]] != expectedComment {
		return fmt.Errorf("面试卡片已变化，请刷新后重试")
	}
	var current *InterviewCard
	cards, _ := parseWorkbenchCards(file)
	for i := range cards {
		if cards[i].LineIndex == lineIndex {
			current = &cards[i]
			break
		}
	}
	if current == nil {
		return fmt.Errorf("面试卡片格式无效，请检查原文")
	}
	failures := 0
	if level == "不会" {
		failures = current.Failures + 1
	}
	updates := map[string]any{
		"level": level, "interval": interval, "due": day.AddDate(0, 0, interval).Format("2006-01-02"),
		"reps": current.Reps + 1, "failures": failures, "lastReviewed": date, "weak": failures >= 2,
	}
	raw := line[match[2]:match[3]]
	var original map[string]json.RawMessage
	if err := json.Unmarshal([]byte(raw), &original); err != nil {
		return err
	}
	// Keep recognized legacy spellings consistent if the note already uses them.
	if _, exists := original["last_reviewed"]; exists {
		updates["last_reviewed"] = date
	}
	if _, exists := original["consecutiveFailures"]; exists {
		updates["consecutiveFailures"] = failures
	}
	patched, err := patchWorkbenchJSONObject(raw, updates)
	if err != nil {
		return err
	}
	file.lines[lineIndex].text = line[:match[2]] + patched + line[match[3]:]
	change.after = joinWorkbenchLines(file.lines)
	return commitWorkbenchChanges(change)
}

// Decoder offsets locate top-level values, leaving field order, whitespace,
// large numbers and nested unknown values byte-for-byte intact.
func patchWorkbenchJSONObject(raw string, updates map[string]any) (string, error) {
	decoder := json.NewDecoder(strings.NewReader(raw))
	token, err := decoder.Token()
	if err != nil || token != json.Delim('{') {
		return "", fmt.Errorf("SRS 数据必须是 JSON 对象")
	}
	seen := make(map[string]bool)
	var out strings.Builder
	cursor, count := 0, 0
	insertion := int(decoder.InputOffset())
	for decoder.More() {
		token, err := decoder.Token()
		if err != nil {
			return "", err
		}
		key, ok := token.(string)
		if !ok {
			return "", fmt.Errorf("无效 SRS 属性名")
		}
		start := int(decoder.InputOffset())
		for start < len(raw) && (raw[start] == ':' || raw[start] == ' ' || raw[start] == '\t' || raw[start] == '\r' || raw[start] == '\n') {
			start++
		}
		var value json.RawMessage
		if err := decoder.Decode(&value); err != nil {
			return "", err
		}
		end := int(decoder.InputOffset())
		insertion = end
		if update, exists := updates[key]; exists {
			encoded, err := json.Marshal(update)
			if err != nil {
				return "", err
			}
			out.WriteString(raw[cursor:start])
			out.Write(encoded)
			cursor = end
		}
		seen[key] = true
		count++
	}
	if _, err := decoder.Token(); err != nil {
		return "", err
	}
	out.WriteString(raw[cursor:insertion])
	colon, comma := ":", ","
	if strings.Contains(raw, `": `) {
		colon = ": "
	}
	if strings.Contains(raw, ", ") {
		comma = ", "
	}
	for _, key := range []string{"level", "interval", "due", "reps", "failures", "lastReviewed", "weak"} {
		update, exists := updates[key]
		if seen[key] || !exists {
			continue
		}
		if count > 0 {
			out.WriteString(comma)
		}
		keyJSON, _ := json.Marshal(key)
		valueJSON, err := json.Marshal(update)
		if err != nil {
			return "", err
		}
		out.Write(keyJSON)
		out.WriteString(colon)
		out.Write(valueJSON)
		count++
	}
	out.WriteString(raw[insertion:])
	return out.String(), nil
}
