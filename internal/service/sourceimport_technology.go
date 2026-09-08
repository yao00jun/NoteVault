package service

import (
	"encoding/json"
	"encoding/xml"
	"path"
	"regexp"
	"sort"
	"strings"
)

var sourceGoModule = regexp.MustCompile(`(?m)^\s*module\s+\S+`)
var sourceRustManifest = regexp.MustCompile(`(?m)^\s*\[(package|workspace)\]\s*(#.*)?$`)
var sourcePythonManifest = regexp.MustCompile(`(?m)^\s*\[(project|build-system|tool\.poetry)\]\s*(#.*)?$`)

// Inference describes declared technologies, not project state or progress.
// Small known manifests are sufficient; arbitrary prose and model output never
// become metadata. A malformed/oversized manifest is still imported normally.
func sourceInferTechnologies(name string, data []byte) []string {
	if len(data) == 0 || len(data) > 512<<10 {
		return nil
	}
	text, err := sourceDecodeText(data)
	if err != nil {
		return nil
	}
	technologies := map[string]bool{}
	add := func(values ...string) {
		for _, value := range values {
			technologies[value] = true
		}
	}
	switch strings.ToLower(path.Base(name)) {
	case "package.json":
		var manifest struct {
			Dependencies    map[string]string `json:"dependencies"`
			DevDependencies map[string]string `json:"devDependencies"`
		}
		if json.Unmarshal([]byte(text), &manifest) != nil || len(manifest.Dependencies)+len(manifest.DevDependencies) > 2000 {
			return nil
		}
		known := map[string]string{
			"vue": "Vue", "typescript": "TypeScript", "react": "React", "next": "Next.js", "nuxt": "Nuxt",
			"@angular/core": "Angular", "svelte": "Svelte", "@sveltejs/kit": "SvelteKit", "vite": "Vite",
			"express": "Express", "fastify": "Fastify", "@nestjs/core": "NestJS", "electron": "Electron",
		}
		for _, dependencies := range []map[string]string{manifest.Dependencies, manifest.DevDependencies} {
			for dependency, version := range dependencies {
				if label := known[dependency]; label != "" && strings.TrimSpace(version) != "" {
					add(label)
				}
			}
		}
	case "go.mod":
		if sourceGoModule.MatchString(text) {
			add("Go")
		}
	case "cargo.toml":
		if sourceRustManifest.MatchString(text) {
			add("Rust")
		}
	case "pyproject.toml":
		if sourcePythonManifest.MatchString(text) {
			add("Python")
		}
	case "requirements.txt", "requirements-dev.txt", "requirements-test.txt":
		known := map[string]string{"django": "Django", "flask": "Flask", "fastapi": "FastAPI", "numpy": "NumPy", "pandas": "pandas", "torch": "PyTorch", "tensorflow": "TensorFlow"}
		for _, line := range strings.Split(text, "\n") {
			line = strings.TrimSpace(strings.SplitN(line, "#", 2)[0])
			if line == "" || strings.HasPrefix(line, "-") {
				continue
			}
			dependency := strings.FieldsFunc(line, func(r rune) bool { return strings.ContainsRune("<>=!~[; \t", r) })
			if len(dependency) > 0 {
				add("Python")
				if label := known[strings.ToLower(dependency[0])]; label != "" {
					add(label)
				}
			}
		}
	case "pom.xml":
		var manifest struct {
			XMLName xml.Name `xml:"project"`
			Parent  struct {
				Group string `xml:"groupId"`
			} `xml:"parent"`
			Dependencies []struct {
				Group string `xml:"groupId"`
			} `xml:"dependencies>dependency"`
		}
		if xml.Unmarshal([]byte(text), &manifest) == nil {
			add("Java", "Maven")
			if manifest.Parent.Group == "org.springframework.boot" {
				add("Spring Boot")
			}
			for _, dependency := range manifest.Dependencies {
				if dependency.Group == "org.springframework.boot" {
					add("Spring Boot")
				}
			}
		}
	case "build.gradle", "build.gradle.kts":
		add("Gradle")
		for _, line := range strings.Split(text, "\n") {
			line = strings.SplitN(line, "//", 2)[0]
			if strings.Contains(line, `"java"`) || strings.Contains(line, "'java'") || strings.Contains(line, `"java-library"`) || strings.Contains(line, "'java-library'") || strings.TrimSpace(line) == "java" {
				add("Java")
			}
			if strings.Contains(line, "org.jetbrains.kotlin.jvm") || strings.Contains(line, `kotlin("jvm")`) || strings.Contains(line, "kotlin('jvm')") {
				add("Kotlin")
			}
			if strings.Contains(line, `"org.springframework.boot"`) || strings.Contains(line, "'org.springframework.boot'") {
				add("Spring Boot")
			}
		}
	}
	result := make([]string, 0, len(technologies))
	for technology := range technologies {
		result = append(result, technology)
	}
	sort.Strings(result)
	return result
}
