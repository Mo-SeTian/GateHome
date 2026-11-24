import assert from 'node:assert/strict'
import { test } from 'node:test'
import {
  getCursorLineCol,
  getLineIndentLevels,
  getLineOffset,
  lintYamlLines,
  parseYamlErrorLocations,
  tokenizeYamlLine
} from './yamlEditor.js'

test('tokenizeYamlLine highlights keys, strings, booleans, numbers, comments, and Mihomo rules', () => {
  const kv = tokenizeYamlLine("    filter: '^(?=.*HK).*$' # hk filter")
  assert.ok(kv.some((t) => t.cls === 'tok-key' && t.text === 'filter'))
  assert.ok(kv.some((t) => t.cls === 'tok-punct' && t.text === ':'))
  assert.ok(kv.some((t) => t.cls === 'tok-str' && t.text === "'^(?=.*HK).*$'"))
  assert.ok(kv.some((t) => t.cls === 'tok-comment' && t.text === '# hk filter'))

  const boolToks = tokenizeYamlLine('    include-all: true')
  assert.ok(boolToks.some((t) => t.cls === 'tok-bool' && t.text === 'true'))

  const numToks = tokenizeYamlLine('    interval: 300')
  assert.ok(numToks.some((t) => t.cls === 'tok-num' && t.text === '300'))

  const ruleToks = tokenizeYamlLine('  - DOMAIN-SUFFIX,anthropic.com,HK 专线,no-resolve')
  assert.ok(ruleToks.some((t) => t.cls === 'tok-rule' && t.text === 'DOMAIN-SUFFIX'))
  assert.ok(ruleToks.some((t) => t.cls === 'tok-target' && t.text === 'HK 专线'))
  assert.ok(ruleToks.some((t) => t.cls === 'tok-bool' && t.text === 'no-resolve'))
})

test('getLineIndentLevels computes 2-space indent guides across blank lines', () => {
  const levels = getLineIndentLevels([
    'proxy-groups:',
    '  - name: OpenAI',
    '    type: fallback',
    '',
    '    url: https://example.com',
    'rules:'
  ])
  assert.deepEqual(levels, [0, 1, 2, 2, 2, 0])
})

test('lintYamlLines flags tabs, unclosed quotes, and unclosed flow brackets without false-positiving regexes', () => {
  const validRegexYaml = [
    'proxy-groups:',
    "  - name: '香港专线'",
    '    type: fallback',
    "    filter: '^(?=.*(?<!深)(港|HK|(?i)Hong))(?!(.*(台|本|坡|美))).*$'",
    '    url: https://example.com/path[1]/ok'
  ].join('\n')
  assert.deepEqual(lintYamlLines(validRegexYaml), [])

  const broken = [
    'proxy-groups:',
    '\t- name: badtab',
    "  - name: 'unclosed",
    '    proxies: [DIRECT, HK'
  ].join('\n')
  const issues = lintYamlLines(broken)
  assert.equal(issues.length, 3)
  assert.deepEqual(
    issues.map((i) => i.line),
    [2, 3, 4]
  )
})

test('parseYamlErrorLocations resolves explicit line numbers and semantic Mihomo error targets', () => {
  const sample = [
    'mixed-port: bad',
    'proxy-groups:',
    '  - name: HK',
    '    type: select',
    '    proxies: [MISSING_NODE]',
    'rules:',
    '  - BAD_RULE,x'
  ].join('\n')

  const explicit = parseYamlErrorLocations(
    'YAML 解析失败: yaml: unmarshal errors:\n  line 3: field icon not found\n  line 5: invalid field',
    sample
  )
  assert.deepEqual(
    explicit.map((e) => e.line),
    [3, 5]
  )

  const chineseLine = parseYamlErrorLocations('解析失败：第 4 行缩进有误', sample)
  assert.deepEqual(
    chineseLine.map((e) => e.line),
    [4]
  )

  const semanticRef = parseYamlErrorLocations('策略组「HK」引用了不存在的节点或策略组「MISSING_NODE」', sample)
  assert.equal(semanticRef[0].line, 5)

  const semanticPort = parseYamlErrorLocations('mixed-port 须为 0–65535 的整数', sample)
  assert.equal(semanticPort[0].line, 1)
})

test('getCursorLineCol and getLineOffset round-trip line positions', () => {
  const doc = 'alpha\nbeta\ngamma'
  assert.deepEqual(getCursorLineCol(doc, getLineOffset(doc, 3) + 2), { line: 3, col: 3 })
  assert.deepEqual(getCursorLineCol(doc, 0), { line: 1, col: 1 })
})
