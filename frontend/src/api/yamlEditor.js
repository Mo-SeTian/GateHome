const MIHOMO_RULE_KEYWORDS = new Set([
  'DOMAIN',
  'DOMAIN-SUFFIX',
  'DOMAIN-KEYWORD',
  'DOMAIN-REGEX',
  'GEOSITE',
  'IP-CIDR',
  'IP-CIDR6',
  'IP-SUFFIX',
  'IP-ASN',
  'GEOIP',
  'SRC-GEOIP',
  'SRC-IP-ASN',
  'SRC-IP-CIDR',
  'SRC-IP-SUFFIX',
  'DST-PORT',
  'SRC-PORT',
  'IN-PORT',
  'IN-TYPE',
  'IN-USER',
  'IN-NAME',
  'PROCESS-NAME',
  'PROCESS-PATH',
  'PROCESS-PATH-REGEX',
  'NETWORK',
  'UID',
  'DSCP',
  'RULE-SET',
  'AND',
  'OR',
  'NOT',
  'SUB-RULE',
  'MATCH'
])

const MIHOMO_TARGET_KEYWORDS = new Set(['DIRECT', 'REJECT', 'REJECT-DROP', 'PASS', 'COMPATIBLE'])

export function classifyYamlScalar(val) {
  const trimmed = String(val || '').trim()
  if (!trimmed) return 'tok-val'
  if (/^(true|false|null|~|yes|no|on|off)$/i.test(trimmed)) return 'tok-bool'
  if (/^-?\d+(\.\d+)?$/.test(trimmed)) return 'tok-num'
  if (
    (trimmed.startsWith('"') && trimmed.endsWith('"')) ||
    (trimmed.startsWith("'") && trimmed.endsWith("'"))
  ) {
    return 'tok-str'
  }
  if (/^[&*][A-Za-z0-9_-]+/.test(trimmed) || trimmed === '<<') return 'tok-anchor'
  if (/^https?:\/\//i.test(trimmed)) return 'tok-str'
  return 'tok-val'
}

function splitCommentIndex(line) {
  let quote = ''
  for (let i = 0; i < line.length; i++) {
    const ch = line[i]
    if (quote === "'") {
      if (ch === "'") {
        if (line[i + 1] === "'") i++
        else quote = ''
      }
    } else if (quote === '"') {
      if (ch === '"' && line[i - 1] !== '\\') quote = ''
    } else if (ch === '"' || ch === "'") {
      quote = ch
    } else if (ch === '#' && (i === 0 || /\s/.test(line[i - 1]))) {
      return i
    }
  }
  return -1
}

function tokenizeInlineList(text, tokens) {
  const open = text[0]
  const close = open === '[' ? ']' : '}'
  const endIdx = text.lastIndexOf(close)
  if (endIdx <= 0) {
    tokens.push({ cls: classifyYamlScalar(text), text })
    return
  }
  const inner = text.slice(1, endIdx)
  const tail = text.slice(endIdx + 1)
  tokens.push({ cls: 'tok-punct', text: open })
  let i = 0
  while (i < inner.length) {
    if (/\s/.test(inner[i])) {
      let j = i
      while (j < inner.length && /\s/.test(inner[j])) j++
      tokens.push({ cls: 'tok-ws', text: inner.slice(i, j) })
      i = j
      continue
    }
    if (inner[i] === ',') {
      tokens.push({ cls: 'tok-punct', text: ',' })
      i++
      continue
    }
    if (inner[i] === '"' || inner[i] === "'") {
      const q = inner[i]
      let j = i + 1
      while (j < inner.length) {
        if (q === "'" && inner[j] === "'") {
          if (inner[j + 1] === "'") j += 2
          else {
            j++
            break
          }
        } else if (q === '"' && inner[j] === '"' && inner[j - 1] !== '\\') {
          j++
          break
        } else {
          j++
        }
      }
      tokens.push({ cls: 'tok-str', text: inner.slice(i, j) })
      i = j
      continue
    }
    let j = i
    while (j < inner.length && inner[j] !== ',') j++
    const part = inner.slice(i, j)
    const trimmed = part.trim()
    const lead = part.slice(0, part.indexOf(trimmed))
    const trailStart = part.indexOf(trimmed) + trimmed.length
    const trail = part.slice(trailStart)
    if (lead) tokens.push({ cls: 'tok-ws', text: lead })
    if (trimmed) {
      const cls = MIHOMO_TARGET_KEYWORDS.has(trimmed) ? 'tok-target' : classifyYamlScalar(trimmed)
      tokens.push({ cls, text: trimmed })
    }
    if (trail) tokens.push({ cls: 'tok-ws', text: trail })
    i = j
  }
  tokens.push({ cls: 'tok-punct', text: close })
  if (tail) tokens.push({ cls: 'tok-val', text: tail })
}

function tokenizeRuleScalar(text, tokens) {
  const parts = text.split(',')
  const head = parts[0].trim().toUpperCase()
  if (parts.length < 2 || !MIHOMO_RULE_KEYWORDS.has(head)) return false
  for (let i = 0; i < parts.length; i++) {
    if (i > 0) tokens.push({ cls: 'tok-punct', text: ',' })
    const part = parts[i]
    const trimmed = part.trim()
    if (!trimmed) {
      if (part) tokens.push({ cls: 'tok-ws', text: part })
      continue
    }
    const lead = part.slice(0, part.indexOf(trimmed))
    const trail = part.slice(part.indexOf(trimmed) + trimmed.length)
    if (lead) tokens.push({ cls: 'tok-ws', text: lead })
    let cls = 'tok-val'
    if (i === 0) cls = 'tok-rule'
    else if (trimmed.toLowerCase() === 'no-resolve') cls = 'tok-bool'
    else if (MIHOMO_TARGET_KEYWORDS.has(trimmed)) cls = 'tok-target'
    else if (i === parts.length - 1 && head === 'MATCH') cls = 'tok-target'
    else if (i === 1 && (head === 'DST-PORT' || head === 'SRC-PORT' || /^\d+$/.test(trimmed))) cls = 'tok-num'
    else if (i === parts.length - 1 || (parts.length >= 4 && i === 2)) cls = 'tok-target'
    tokens.push({ cls, text: trimmed })
    if (trail) tokens.push({ cls: 'tok-ws', text: trail })
  }
  return true
}

function tokenizeScalarValue(rawValue, tokens) {
  const match = rawValue.match(/^(\s*)(.*?)(\s*)$/)
  if (!match) return
  const [, lead, core, trail] = match
  if (lead) tokens.push({ cls: 'tok-ws', text: lead })
  if (core) {
    if ((core.startsWith('[') && core.endsWith(']')) || (core.startsWith('{') && core.endsWith('}'))) {
      tokenizeInlineList(core, tokens)
    } else if (!tokenizeRuleScalar(core, tokens)) {
      const cls = MIHOMO_TARGET_KEYWORDS.has(core) ? 'tok-target' : classifyYamlScalar(core)
      tokens.push({ cls, text: core })
    }
  }
  if (trail) tokens.push({ cls: 'tok-ws', text: trail })
}

function findUnquotedColonIndex(text) {
  let quote = ''
  for (let i = 0; i < text.length; i++) {
    const ch = text[i]
    if (quote === "'") {
      if (ch === "'") {
        if (text[i + 1] === "'") i++
        else quote = ''
      }
    } else if (quote === '"') {
      if (ch === '"' && text[i - 1] !== '\\') quote = ''
    } else if (ch === '"' || ch === "'") {
      quote = ch
    } else if (ch === ':' && (i + 1 === text.length || /\s/.test(text[i + 1]))) {
      return i
    }
  }
  return -1
}

export function tokenizeYamlLine(line) {
  if (!line) return []
  const commentIdx = splitCommentIndex(line)
  const code = commentIdx >= 0 ? line.slice(0, commentIdx) : line
  const comment = commentIdx >= 0 ? line.slice(commentIdx) : ''
  const tokens = []

  if (code) {
    const listMatch = code.match(/^(\s*-\s+)(.*)$/)
    let rest = code
    if (listMatch) {
      const dashLead = listMatch[1].match(/^(\s*)(-\s+)$/)
      if (dashLead?.[1]) tokens.push({ cls: 'tok-ws', text: dashLead[1] })
      tokens.push({ cls: 'tok-punct', text: dashLead ? dashLead[2] : listMatch[1] })
      rest = listMatch[2]
    }
    const colonIdx = findUnquotedColonIndex(rest)
    if (colonIdx > 0) {
      const keyPart = rest.slice(0, colonIdx)
      const afterColon = rest.slice(colonIdx + 1)
      const keyLead = keyPart.match(/^(\s*)(.*)$/)
      if (keyLead?.[1]) tokens.push({ cls: 'tok-ws', text: keyLead[1] })
      tokens.push({ cls: 'tok-key', text: keyLead ? keyLead[2] : keyPart })
      tokens.push({ cls: 'tok-punct', text: ':' })
      if (afterColon) tokenizeScalarValue(afterColon, tokens)
    } else if (rest) {
      tokenizeScalarValue(rest, tokens)
    }
  }
  if (comment) {
    tokens.push({ cls: 'tok-comment', text: comment })
  }
  return tokens
}

export function getLineIndentLevels(lines) {
  const raw = lines.map((line) => {
    const trimmed = line.trim()
    if (!trimmed) return -1
    const match = line.match(/^( +)/)
    if (!match) return 0
    return Math.floor(match[1].length / 2)
  })
  const result = new Array(lines.length).fill(0)
  for (let i = 0; i < raw.length; i++) {
    if (raw[i] >= 0) {
      result[i] = raw[i]
      continue
    }
    let next = 0
    for (let j = i + 1; j < raw.length; j++) {
      if (raw[j] >= 0) {
        next = raw[j]
        break
      }
    }
    const prev = i > 0 ? result[i - 1] : 0
    result[i] = Math.min(prev, next)
  }
  return result
}

export function lintYamlLines(content) {
  const lines = String(content ?? '').split('\n')
  const errors = []
  let flowBalance = 0

  for (let idx = 0; idx < lines.length; idx++) {
    const lineNum = idx + 1
    const line = lines[idx]

    if (/^\t/.test(line) || /^ *\t/.test(line)) {
      errors.push({
        line: lineNum,
        message: 'YAML 缩进不能使用制表符 (Tab)，请改用空格',
        severity: 'error'
      })
    }

    const commentIdx = splitCommentIndex(line)
    const code = commentIdx >= 0 ? line.slice(0, commentIdx) : line
    if (!code.trim()) continue

    // Scan quotes & flow brackets strictly when in a YAML flow context or after key/list separator.
    let quote = ''
    let inFlowScan = flowBalance > 0
    for (let i = 0; i < code.length; i++) {
      const ch = code[i]
      if (quote === "'") {
        if (ch === "'") {
          if (code[i + 1] === "'") i++
          else quote = ''
        }
        continue
      }
      if (quote === '"') {
        if (ch === '"' && code[i - 1] !== '\\') quote = ''
        continue
      }
      if (ch === '"' || ch === "'") {
        // Only treat leading quotes of a scalar token as YAML string delimiters.
        const prev = i === 0 ? ' ' : code[i - 1]
        if (/\s|[:[\]{},]/.test(prev)) quote = ch
        continue
      }
      if (!inFlowScan) {
        if ((ch === ':' && /\s/.test(code[i + 1] || ' ')) || (ch === '-' && /\s/.test(code[i + 1] || '') && /^\s*-/.test(code.slice(0, i + 1)))) {
          const tail = code.slice(i + 1).trimStart()
          if (tail.startsWith('[') || tail.startsWith('{')) inFlowScan = true
        } else if (/^\s*[\[{]/.test(code)) {
          inFlowScan = true
        }
      }
      if (inFlowScan) {
        if (ch === '[' || ch === '{') flowBalance++
        else if (ch === ']' || ch === '}') flowBalance--
      }
    }

    if (quote) {
      errors.push({
        line: lineNum,
        message: `引号 (${quote}) 未闭合`,
        severity: 'error'
      })
    }
    if (flowBalance < 0) {
      errors.push({
        line: lineNum,
        message: '发现多余的闭合括号 ] 或 }',
        severity: 'error'
      })
      flowBalance = 0
    }
  }

  if (flowBalance > 0) {
    errors.push({
      line: lines.length || 1,
      message: '存在未闭合的 [ ] 或 { } 流式列表/映射',
      severity: 'error'
    })
  }
  return errors
}

function findNthMatchLine(lines, regex, occurrence = 1) {
  let count = 0
  for (let i = 0; i < lines.length; i++) {
    if (regex.test(lines[i])) {
      count++
      if (count === occurrence) return i + 1
    }
  }
  return null
}

function escapeRegExp(str) {
  return str.replace(/[.*+?^${}()|[\]\\]/g, '\\$&')
}

export function parseYamlErrorLocations(errorText, content) {
  const msg = String(errorText || '').trim()
  if (!msg) return []
  const lines = String(content ?? '').split('\n')
  const locations = []
  const seenLines = new Set()

  const addLoc = (lineNum, detail) => {
    if (!lineNum || lineNum < 1 || lineNum > lines.length || seenLines.has(lineNum)) return
    seenLines.add(lineNum)
    locations.push({ line: lineNum, message: detail || msg, severity: 'error' })
  }

  // 1. Explicit `line N:` or `第 N 行` markers (supports multiple yaml.v3 unmarshal error lines)
  const explicitMatches = [...msg.matchAll(/(?:\bline\s+|第\s*)(\d+)(?:\s*[:：]|\s*行)/gi)]
  for (const m of explicitMatches) {
    const lineNum = Number(m[1])
    addLoc(lineNum, msg)
  }
  if (locations.length > 0) return locations

  // 2. Semantic fallback: `第 N 个策略组「NAME」`
  const groupOrdinal = msg.match(/第\s*(\d+)\s*个策略组「([^」]*)」/)
  if (groupOrdinal) {
    const name = groupOrdinal[2].trim()
    if (name) {
      const byName = findNthMatchLine(
        lines,
        new RegExp(`^\\s*-\\s*name\\s*:\\s*['"]?${escapeRegExp(name)}['"]?\\s*(?:#.*)?$`),
        1
      )
      if (byName) addLoc(byName, msg)
    }
    if (locations.length === 0) {
      const idxLine = findNthMatchLine(lines, /^\s*-\s*name\s*:/, Number(groupOrdinal[1]))
      if (idxLine) addLoc(idxLine, msg)
    }
    if (locations.length > 0) return locations
  }

  // 3. Semantic fallback: `策略组「GROUP」引用了不存在的节点或策略组「TARGET」`
  const groupRef = msg.match(/策略组「([^」]+)」.*「([^」]+)」/)
  if (groupRef) {
    const [, groupName, targetName] = groupRef
    const startLine = findNthMatchLine(
      lines,
      new RegExp(`^\\s*-\\s*name\\s*:\\s*['"]?${escapeRegExp(groupName)}['"]?`),
      1
    )
    if (startLine) {
      const targetRegex = new RegExp(escapeRegExp(targetName))
      for (let i = startLine - 1; i < lines.length; i++) {
        if (i > startLine - 1 && /^\s*-\s*name\s*:/.test(lines[i])) break
        if (targetRegex.test(lines[i])) {
          addLoc(i + 1, msg)
          return locations
        }
      }
      addLoc(startLine, msg)
      return locations
    }
  }

  // 4. Semantic fallback: `第 N 条规则`
  const ruleOrdinal = msg.match(/第\s*(\d+)\s*条规则/)
  if (ruleOrdinal) {
    const rulesHeader = findNthMatchLine(lines, /^rules\s*:/, 1) || 0
    const targetIdx = Number(ruleOrdinal[1])
    let seen = 0
    for (let i = rulesHeader; i < lines.length; i++) {
      if (/^\s*-\s+/.test(lines[i])) {
        seen++
        if (seen === targetIdx) {
          addLoc(i + 1, msg)
          return locations
        }
      }
    }
  }

  // 5. Semantic fallback: known top-level YAML key mentioned in error
  for (const key of ['mixed-port', 'socks-port', 'port', 'external-controller', 'dns.listen', 'proxy-groups', 'rule-providers', 'proxy-providers', 'rules']) {
    if (msg.includes(key)) {
      const keyLine = findNthMatchLine(lines, new RegExp(`^\\s*${escapeRegExp(key.split('.')[0])}\\s*:`), 1)
      if (keyLine) {
        addLoc(keyLine, msg)
        return locations
      }
    }
  }

  return locations
}

export function getCursorLineCol(content, selectionStart = 0) {
  const text = String(content ?? '')
  const pos = Math.max(0, Math.min(Number(selectionStart) || 0, text.length))
  const prefix = text.slice(0, pos)
  const lines = prefix.split('\n')
  return {
    line: lines.length,
    col: lines[lines.length - 1].length + 1
  }
}

export function getLineOffset(content, lineNumber) {
  const lines = String(content ?? '').split('\n')
  const target = Math.max(1, Math.min(Number(lineNumber) || 1, lines.length))
  let offset = 0
  for (let i = 0; i < target - 1; i++) {
    offset += lines[i].length + 1
  }
  return offset
}
