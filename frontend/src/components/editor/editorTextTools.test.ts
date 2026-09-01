import { describe, expect, it } from 'vitest'
import { transformText } from './editorTextTools'

describe('transformText', () => {
  it('removeBlankLines 删除空行', () => {
    expect(transformText('removeBlankLines', 'a\n\nb\n  \nc')).toBe('a\nb\nc')
  })

  it('insertBlankLines 行间插入空行', () => {
    expect(transformText('insertBlankLines', 'a\nb')).toBe('a\n\nb')
  })

  it('splitLines 按中英文标点切句', () => {
    expect(transformText('splitLines', '第一句。第二句！third;四')).toBe('第一句\n第二句\nthird\n四')
  })

  it('mergeLines 合并为一行', () => {
    expect(transformText('mergeLines', ' a \n\nb\n c ')).toBe('a b c')
  })

  it('dedupeLines 去重（按 trim 后比较，保留首次出现）', () => {
    expect(transformText('dedupeLines', 'a\nb\n a \nc')).toBe('a\nb\nc')
  })

  it('sortLines 按本地化规则排序', () => {
    // 不断言具体中文顺序：node 测试环境可能缺 zh ICU 数据，localeCompare 会退化为恒 0
    expect(transformText('sortLines', 'b\na\nc')).toBe('a\nb\nc')
  })

  it('fullHalfConvert 全角转半角（含全角空格）', () => {
    expect(transformText('fullHalfConvert', 'ＡＢＣ１２！　ｘ')).toBe('ABC12! x')
  })

  it('numberLines 加行号', () => {
    expect(transformText('numberLines', 'a\nb')).toBe('1. a\n2. b')
  })

  it('trimLineEnds 去行尾空白', () => {
    expect(transformText('trimLineEnds', 'a \nb\t')).toBe('a\nb')
  })

  it('shrinkSpaces 压缩连续空格/制表符', () => {
    expect(transformText('shrinkSpaces', 'a  \t b')).toBe('a b')
  })

  it('removeAllWhitespace 删除所有空白', () => {
    expect(transformText('removeAllWhitespace', 'a b\nc')).toBe('abc')
  })

  it('listToTable 列表转表格（去掉列表标记）', () => {
    expect(transformText('listToTable', '- 甲\n* 乙\n1. 丙')).toBe('| 项 |\n| --- |\n| 甲 |\n| 乙 |\n| 丙 |')
  })

  it('listToTable 空内容原样返回', () => {
    expect(transformText('listToTable', '')).toBe('')
  })

  it('tableToList 表格转列表（跳过分隔行）', () => {
    expect(transformText('tableToList', '| a | b |\n| --- | --- |\n| c | d |')).toBe('- a / b\n- c / d')
  })

  it('未知 id 原样返回', () => {
    expect(transformText('nope', 'a\nb')).toBe('a\nb')
  })
})
