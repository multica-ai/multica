function closing(text, start, open, close) {
  let depth = 1;
  for (let i = start + 1; i < text.length; i++) {
    if (text[i] === '\\') { i++; continue; }
    if (text[i] === open) depth++;
    else if (text[i] === close && --depth === 0) return i;
  }
  return -1;
}

// Extract display labels without interpreting targets, HTML or Markdown.
// Consume each balanced span once, including nested/escaped URL parentheses.
function linkLabels(text, references, preserveMalformed = false) {
  const result = [];
  for (let i = 0; i < text.length;) {
    if (text[i] === '\\') { result.push(text.slice(i, i + 2)); i += 2; continue; }
    const start = text[i] === '!' && text[i + 1] === '[' ? i + 1 : i;
    if (text[start] !== '[') { result.push(text[i++]); continue; }
    const labelEnd = closing(text, start, '[', ']');
    if (labelEnd < 0) { result.push(text.slice(i)); break; }
    const label = text.slice(start + 1, labelEnd);
    const referenceEnd = text[labelEnd + 1] === '[' ? closing(text, labelEnd + 1, '[', ']') : -1;
    const reference = referenceEnd < 0 ? label : text.slice(labelEnd + 2, referenceEnd) || label;
    if (text[labelEnd + 1] !== '(' && references.has(reference.toLowerCase().replace(/\s+/g, ' ').trim())) {
      result.push(label); i = referenceEnd < 0 ? labelEnd + 1 : referenceEnd + 1; continue;
    }
    const hasTarget = text[labelEnd + 1] === '(';
    const targetEnd = hasTarget ? closing(text, labelEnd + 1, '(', ')') : -1;
    if (hasTarget && targetEnd < 0) { result.push(preserveMalformed ? text.slice(i) : text.slice(start + 1, labelEnd)); break; }
    if (preserveMalformed && targetEnd >= 0 && !/^\s*<?[a-z][a-z0-9+.-]*:/i.test(text.slice(labelEnd + 2, targetEnd))) {
      result.push(text.slice(i, targetEnd + 1)); i = targetEnd + 1; continue;
    }
    if (targetEnd < 0) { result.push(text.slice(i, labelEnd + 1)); i = labelEnd + 1; continue; }
    result.push(text.slice(start + 1, labelEnd)); i = targetEnd + 1;
  }
  return result.join('');
}

function htmlText(text) {
  const result = [];
  for (let i = 0; i < text.length;) {
    if (text[i] !== '<') { result.push(text[i++]); continue; }
    if (text.startsWith('<!--', i)) {
      const end = text.indexOf('-->', i + 4);
      if (end < 0) break;
      result.push(' '); i = end + 3; continue;
    }
    const tail = text.slice(i);
    const tag = /^<\/?([a-z][a-z0-9:-]*)(?=[\s/>])/i.exec(tail);
    const autolink = /^<[a-z][a-z0-9+.-]*:[^<>\s]*>/i.exec(tail);
    if (!tag && autolink) { i += autolink[0].length; continue; }
    if (!tag) { result.push(text[i++]); continue; }
    let quote = null, end = i + tag[0].length;
    for (; end < text.length; end++) {
      const character = text[end];
      if (quote) { if (character === quote) quote = null; }
      else if (character === '"' || character === "'") quote = character;
      else if (character === '>') break;
    }
    if (end === text.length) break;
    const name = tag[1].toLowerCase();
    if (text[i + 1] !== '/' && (name === 'script' || name === 'style')) {
      const close = new RegExp('</\\s*' + name + '\\s*>', 'ig');
      close.lastIndex = end + 1;
      const match = close.exec(text);
      if (!match) break;
      result.push(' '); i = close.lastIndex;
    } else i = end + 1;
  }
  return result.join('');
}

export function trajectoryTitleText(value) {
  if (typeof value !== 'string') return '';
  const references = new Set();
  const withoutDefinitions = value.replace(/^[ \t]{0,3}\[([^\]\r\n]+)\]:[^\r\n]*(?:\r?\n|$)/gm, (_, label) => {
    references.add(label.toLowerCase().replace(/\s+/g, ' ').trim()); return '';
  });
  const plain = linkLabels(htmlText(withoutDefinitions), references).replace(/\\([\x21-\x2f\x3a-\x40\x5b-\x60\x7b-\x7e])/g, '$1')
    // eslint-disable-next-line no-control-regex -- intentional: stored agent output may carry control characters
    .replace(/[\u0000-\u0008\u000b\u000c\u000e-\u001f\u007f]/g, '')
    .replace(/[ \t]+/g, ' ').trim();
  return plain;
}

// Bodies remain inert text: protect code verbatim and only project ordinary
// Markdown links outside code. HTML-looking business syntax is never stripped.
function bodyParts(text) {
  const parts = [];
  const tick = String.fromCharCode(96);
  let plainStart = 0;
  for (let i = 0; i < text.length;) {
    let end = -1;
    if (i === 0 || text[i - 1] === '\n') {
      const lineEnd = text.indexOf('\n', i);
      const line = text.slice(i, lineEnd < 0 ? text.length : lineEnd);
      if (/^( {4}|\t)/.test(line)) end = lineEnd < 0 ? text.length : lineEnd + 1;
      const opening = /^[ \t]{0,3}([\x60~]{3,})/.exec(line);
      if (opening && new Set(opening[1]).size === 1) {
        const character = opening[1][0], size = opening[1].length;
        end = text.length;
        for (let cursor = lineEnd < 0 ? text.length : lineEnd + 1; cursor < text.length;) {
          const next = text.indexOf('\n', cursor);
          const current = text.slice(cursor, next < 0 ? text.length : next);
          const close = /^[ \t]{0,3}([\x60~]+)[ \t]*$/.exec(current);
          if (close && close[1].length >= size && [...close[1]].every(value => value === character)) {
            end = next < 0 ? text.length : next + 1; break;
          }
          cursor = next < 0 ? text.length : next + 1;
        }
      }
    }
    if (end < 0 && text[i] === '<' && /^<\/?[a-z]/i.test(text.slice(i))) {
      let quote = null, cursor = i + 1;
      for (; cursor < text.length; cursor++) {
        if (quote) { if (text[cursor] === quote) quote = null; }
        else if (text[cursor] === '"' || text[cursor] === "'") quote = text[cursor];
        else if (text[cursor] === '>') { end = cursor + 1; break; }
      }
      if (end < 0) end = text.length;
    }
    if (end < 0 && text[i] === tick) {
      let size = 1;
      while (text[i + size] === tick) size++;
      const close = text.indexOf(tick.repeat(size), i + size);
      end = close < 0 ? text.length : close + size;
    }
    if (end >= 0) {
      if (i > plainStart) parts.push({ code: false, text: text.slice(plainStart, i) });
      parts.push({ code: true, text: text.slice(i, end) });
      i = end; plainStart = end;
    } else i++;
  }
  if (plainStart < text.length) parts.push({ code: false, text: text.slice(plainStart) });
  return parts;
}

export function readableTrajectoryText(value) {
  if (typeof value !== 'string') return '';
  try { JSON.parse(value); return value; } catch { /* Not a serialized value. */ }
  const parts = bodyParts(value), references = new Set();
  const prose = parts.map(part => part.code ? part : { ...part, text: part.text.replace(/^[ \t]{0,3}\[([^\]\r\n]+)\]:[^\r\n]*(?:\r?\n|$)/gm, (_, label) => {
    references.add(label.toLowerCase().replace(/\s+/g, ' ').trim()); return '';
  }) });
  return prose.map(part => part.code ? part.text : linkLabels(part.text, references, true)).join('');
}
