package service

import (
	"context"
	"fmt"
	"io"

	"github.com/ledongthuc/pdf"
)

const sourcePDFLayoutLimit = 500000

type sourcePDFLayoutBudget struct {
	ctx       context.Context
	remaining int
}

func (b *sourcePDFLayoutBudget) take(count int) {
	if err := b.ctx.Err(); err != nil {
		panic(err)
	}
	if count < 0 || count > b.remaining {
		panic(fmt.Errorf("PDF 单页布局数据超出上限"))
	}
	b.remaining -= count
}

func (b *sourcePDFLayoutBudget) args(stack *pdf.Stack) []pdf.Value {
	b.take(1 + stack.Len())
	args := make([]pdf.Value, stack.Len())
	for i := len(args) - 1; i >= 0; i-- {
		args[i] = stack.Pop()
	}
	return args
}

// A ToUnicode entry may map one source byte to several Unicode characters.
// Bound that expansion from its UTF-16 string lengths, without decoding and
// allocating the expanded text. Including metadata/source strings only
// overestimates. Lexing avoids executing the CMap's PostScript prologue.
func sourcePDFFontExpansion(b *sourcePDFLayoutBudget, font pdf.Font) int {
	mapping := font.V.Key("ToUnicode")
	if mapping.Kind() != pdf.Stream {
		return 1
	}
	data, err := io.ReadAll(io.LimitReader(mapping.Reader(), sourceMaxFileBytes+1))
	if err != nil {
		panic(err)
	}
	if len(data) > sourceMaxFileBytes {
		panic(fmt.Errorf("PDF 字体映射超出上限"))
	}
	expansion, depth := 1, 0
	for offset := 0; offset < len(data); {
		b.take(1)
		start := sourcePDFSkipSpace(data, offset)
		token, next, err := sourcePDFToken(data, start)
		if err == io.EOF {
			break
		}
		if err != nil {
			panic(err)
		}
		switch token {
		case "<>":
			expansion = max(expansion, (next-start-2+3)/4)
		case "()":
			expansion = max(expansion, (next-start-2+1)/2)
		case "[", "<<":
			depth++
		case "]", ">>":
			depth--
		}
		if depth > 32 {
			panic(fmt.Errorf("PDF 字体映射嵌套过深"))
		}
		offset = next
	}
	return expansion
}

// Content materializes a struct per glyph plus every rectangle and saved
// graphics state. Check a conservative combined budget BEFORE calling it.
// Stream byte limits alone do not bound this per-character amplification.
func sourcePDFCheckLayout(ctx context.Context, page pdf.Page, limit int) (err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			if failure, ok := recovered.(error); ok {
				err = failure
			} else {
				err = fmt.Errorf("PDF 页面布局无效")
			}
		}
	}()
	b := &sourcePDFLayoutBudget{ctx: ctx, remaining: limit}
	b.take(0)
	content := page.V.Key("Contents")
	if content.IsNull() {
		return nil
	}
	fonts := map[string]int{}
	expansion, depth := 1, 0
	text := func(value pdf.Value) {
		raw := value.RawString()
		if len(raw) > b.remaining/expansion {
			panic(fmt.Errorf("PDF 单页布局数据超出上限"))
		}
		b.take(len(raw) * expansion)
	}
	pdf.Interpret(content, func(stack *pdf.Stack, op string) {
		args := b.args(stack)
		switch op {
		case "q":
			depth++
			if depth > 128 {
				panic(fmt.Errorf("PDF 绘图状态嵌套过深"))
			}
		case "Q":
			depth--
			if depth < 0 {
				panic(fmt.Errorf("PDF 绘图状态无效"))
			}
		case "Tf":
			if len(args) != 2 {
				panic(fmt.Errorf("PDF 字体声明无效"))
			}
			name := args[0].Name()
			if fonts[name] == 0 {
				fonts[name] = sourcePDFFontExpansion(b, page.Font(name))
			}
			expansion = fonts[name]
		case "Tj", "'", "\"":
			if len(args) == 0 {
				panic(fmt.Errorf("PDF 文本操作无效"))
			}
			text(args[len(args)-1])
		case "TJ":
			if len(args) != 1 || args[0].Kind() != pdf.Array {
				panic(fmt.Errorf("PDF 文本数组无效"))
			}
			b.take(args[0].Len())
			for i := 0; i < args[0].Len(); i++ {
				value := args[0].Index(i)
				if value.Kind() == pdf.String {
					text(value)
				}
			}
			// Content decodes an extra newline after every TJ, even [] TJ.
			// A CMap can expand that byte into multiple visible glyphs as well.
			b.take(expansion)
		}
	})
	return nil
}
