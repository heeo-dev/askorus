package judge

import "bytes"

// Reading an AI's output: the one JSON object it gave, bare or in a code
// fence (synth.go, refine.go). The judge output's checks went with judging
// (C6d, #24).

// ExtractJSON finds the judge's JSON object in its output. When a line
// opens a ```json fence it reads from after that line, else from the
// start (or again from the start when the fence holds no object). It
// returns the first balanced top-level {…}, reading strings as strings:
// the span of the first "{" whose "}" comes, or, when a stray "{" never
// closes, the earliest object that does. obj is a part of out. ok is false
// when there is none, or when out is over MaxOutputBytes.
func ExtractJSON(out []byte) (obj []byte, ok bool) {
	if len(out) > MaxOutputBytes {
		return nil, false
	}
	from := validFence(out)
	start, end, ok := validObject(out, from)
	if !ok && from > 0 {
		start, end, ok = validObject(out, 0)
	}
	if !ok {
		return nil, false
	}
	return out[start:end], true
}

// validFence is where the text after the first ```json line starts, or 0.
// The fence must open its line, so a fence inside a JSON string, which
// cannot hold a line break, does not count.
func validFence(out []byte) int {
	for ls := 0; ls < len(out); {
		le := bytes.IndexByte(out[ls:], '\n')
		next := len(out)
		if le >= 0 {
			le += ls
			next = le + 1
		} else {
			le = len(out)
		}
		line := bytes.TrimSpace(out[ls:le])
		if len(line) == len("```json") && bytes.HasPrefix(line, []byte("```")) && bytes.EqualFold(line[3:], []byte("json")) {
			return next
		}
		ls = next
	}
	return 0
}

// validObject scans out from from for a balanced {…}, in one pass.
func validObject(out []byte, from int) (start, end int, ok bool) {
	var open []int // the "{" not closed yet
	inString, escaped := false, false
	start = -1
	for i := from; i < len(out); i++ {
		c := out[i]
		if inString {
			switch {
			case escaped:
				escaped = false
			case c == '\\':
				escaped = true
			case c == '"':
				inString = false
			}
			continue
		}
		switch c {
		case '"':
			inString = len(open) > 0 // quotes in prose around the object are prose
		case '{':
			open = append(open, i)
		case '}':
			if len(open) == 0 {
				continue
			}
			s := open[len(open)-1]
			open = open[:len(open)-1]
			if len(open) == 0 {
				return s, i + 1, true
			}
			if start < 0 || s < start {
				start, end = s, i+1
			}
		}
	}
	return start, end, start >= 0
}
