package main

import (
	"bytes"
	"strconv"
	"unsafe"
)

type FastFish struct {
	ID    int64
	Ty    int
	X     float64
	Z     float64
	HP    float64
	Flags int
	Dir   float64
}

type FastShot struct {
	ID   int64
	X    float64
	Z    float64
	Kind string
	HP   float64
}

type FastPlayer struct {
	Slot   int
	Combo  int
	HP     float64
	IsDead bool
}

type SnapshotScanner struct {
	Fish      []FastFish
	Shots     []FastShot
	Players   []FastPlayer
	RawEvents [][]byte
}

func NewSnapshotScanner() *SnapshotScanner {
	return &SnapshotScanner{
		Fish:      make([]FastFish, 0, 128),
		Shots:     make([]FastShot, 0, 64),
		Players:   make([]FastPlayer, 0, 16),
		RawEvents: make([][]byte, 0, 8),
	}
}

func b2s(b []byte) string {
	if len(b) == 0 {
		return ""
	}
	return unsafe.String(unsafe.SliceData(b), len(b))
}

func parseFloatBytes(b []byte) float64 {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return 0
	}
	v, _ := strconv.ParseFloat(b2s(b), 64)
	return v
}

func parseIntBytes(b []byte) int64 {
	b = bytes.TrimSpace(b)
	if len(b) == 0 {
		return 0
	}
	v, _ := strconv.ParseInt(b2s(b), 10, 64)
	return v
}

func (s *SnapshotScanner) Reset() {
	s.Fish = s.Fish[:0]
	s.Shots = s.Shots[:0]
	s.Players = s.Players[:0]
	s.RawEvents = s.RawEvents[:0]
}

// skipValue advances past a JSON value (string, array, object, primitive)
func skipValue(data []byte, i int) int {
	n := len(data)
	for i < n && (data[i] == ' ' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
		i++
	}
	if i >= n {
		return i
	}
	switch data[i] {
	case '"':
		i++
		for i < n {
			if data[i] == '\\' && i+1 < n {
				i += 2
				continue
			}
			if data[i] == '"' {
				i++
				return i
			}
			i++
		}
		return i
	case '[', '{':
		open := data[i]
		close := byte(']')
		if open == '{' {
			close = '}'
		}
		depth := 1
		inQuote := false
		i++
		for i < n && depth > 0 {
			c := data[i]
			if c == '\\' && inQuote && i+1 < n {
				i += 2
				continue
			}
			if c == '"' {
				inQuote = !inQuote
			} else if !inQuote {
				if c == open {
					depth++
				} else if c == close {
					depth--
				}
			}
			i++
		}
		return i
	default:
		// number, boolean, null
		for i < n && data[i] != ',' && data[i] != '}' && data[i] != ']' {
			i++
		}
		return i
	}
}

// Parse extracts snapshot arrays with zero heap allocations
func (s *SnapshotScanner) Parse(data []byte) bool {
	s.Reset()
	n := len(data)
	i := 0

	// Find opening '{'
	for i < n && data[i] != '{' {
		i++
	}
	if i >= n {
		return false
	}
	i++

	isSnapshot := false

	for i < n {
		// Skip whitespace, commas
		for i < n && (data[i] == ' ' || data[i] == ',' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
			i++
		}
		if i >= n || data[i] == '}' {
			break
		}

		if data[i] != '"' {
			i++
			continue
		}
		i++ // skip opening quote
		keyStart := i
		for i < n && data[i] != '"' {
			i++
		}
		key := data[keyStart:i]
		if i < n {
			i++ // skip closing quote
		}

		// Skip whitespace and colon
		for i < n && (data[i] == ' ' || data[i] == ':' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
			i++
		}
		if i >= n {
			break
		}

		if len(key) == 1 && key[0] == 't' {
			// Check if t == "s"
			if data[i] == '"' && i+2 < n && data[i+1] == 's' && data[i+2] == '"' {
				isSnapshot = true
				i += 3
				continue
			}
		}

		if len(key) == 1 && key[0] == 'f' && data[i] == '[' {
			i = s.parseFish(data, i)
			continue
		} else if len(key) == 2 && key[0] == 's' && key[1] == 'h' && data[i] == '[' {
			i = s.parseShots(data, i)
			continue
		} else if len(key) == 1 && key[0] == 'p' && data[i] == '[' {
			i = s.parsePlayers(data, i)
			continue
		} else if len(key) == 2 && key[0] == 'e' && key[1] == 'v' && data[i] == '[' {
			i = s.parseEvents(data, i)
			continue
		}

		// Skip unhandled value
		i = skipValue(data, i)
	}

	return isSnapshot
}

func (s *SnapshotScanner) parseFish(data []byte, start int) int {
	i := start + 1
	n := len(data)
	for i < n && data[i] != ']' {
		for i < n && (data[i] == ' ' || data[i] == ',' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
			i++
		}
		if i >= n || data[i] == ']' {
			break
		}

		if data[i] == '[' {
			i++
			var f FastFish
			fieldIdx := 0
			valStart := i
			for i < n && data[i] != ']' {
				if data[i] == ',' {
					assignFishField(&f, fieldIdx, data[valStart:i])
					fieldIdx++
					i++
					valStart = i
					continue
				}
				i++
			}
			if i > valStart {
				assignFishField(&f, fieldIdx, data[valStart:i])
			}
			if f.ID > 0 {
				s.Fish = append(s.Fish, f)
			}
			if i < n && data[i] == ']' {
				i++
			}
			continue
		}
		i++
	}
	if i < n && data[i] == ']' {
		i++
	}
	return i
}

func assignFishField(f *FastFish, idx int, val []byte) {
	switch idx {
	case 0:
		f.ID = parseIntBytes(val)
	case 1:
		f.Ty = int(parseIntBytes(val))
	case 2:
		f.X = parseFloatBytes(val)
	case 3:
		f.Z = parseFloatBytes(val)
	case 4:
		f.HP = parseFloatBytes(val)
	case 5:
		f.Flags = int(parseIntBytes(val))
	case 6:
		f.Dir = parseFloatBytes(val)
	}
}

func (s *SnapshotScanner) parseShots(data []byte, start int) int {
	i := start + 1
	n := len(data)
	for i < n && data[i] != ']' {
		for i < n && (data[i] == ' ' || data[i] == ',' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
			i++
		}
		if i >= n || data[i] == ']' {
			break
		}

		if data[i] == '[' {
			i++
			var shot FastShot
			fieldIdx := 0
			valStart := i
			inQuote := false
			for i < n && (data[i] != ']' || inQuote) {
				if data[i] == '"' && (i == 0 || data[i-1] != '\\') {
					inQuote = !inQuote
				}
				if !inQuote && data[i] == ',' {
					assignShotField(&shot, fieldIdx, data[valStart:i])
					fieldIdx++
					i++
					valStart = i
					continue
				}
				i++
			}
			if i > valStart {
				assignShotField(&shot, fieldIdx, data[valStart:i])
			}
			if shot.ID > 0 {
				s.Shots = append(s.Shots, shot)
			}
			if i < n && data[i] == ']' {
				i++
			}
			continue
		}
		i++
	}
	if i < n && data[i] == ']' {
		i++
	}
	return i
}

func assignShotField(shot *FastShot, idx int, val []byte) {
	switch idx {
	case 0:
		shot.ID = parseIntBytes(val)
	case 1:
		shot.X = parseFloatBytes(val)
	case 2:
		shot.Z = parseFloatBytes(val)
	case 3:
		val = bytes.Trim(val, " \t\r\n\"")
		shot.Kind = b2s(val)
	case 4:
		shot.HP = parseFloatBytes(val)
	}
}

func (s *SnapshotScanner) parsePlayers(data []byte, start int) int {
	i := start + 1
	n := len(data)
	for i < n && data[i] != ']' {
		for i < n && (data[i] == ' ' || data[i] == ',' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
			i++
		}
		if i >= n || data[i] == ']' {
			break
		}

		if data[i] == '[' {
			i++
			var p FastPlayer
			p.Slot = -1
			fieldIdx := 0
			valStart := i
			inQuote := false
			for i < n && (data[i] != ']' || inQuote) {
				if data[i] == '"' && (i == 0 || data[i-1] != '\\') {
					inQuote = !inQuote
				}
				if !inQuote && data[i] == ',' {
					assignPlayerField(&p, fieldIdx, data[valStart:i])
					fieldIdx++
					i++
					valStart = i
					continue
				}
				i++
			}
			if i > valStart {
				assignPlayerField(&p, fieldIdx, data[valStart:i])
			}
			if p.Slot >= 0 {
				s.Players = append(s.Players, p)
			}
			if i < n && data[i] == ']' {
				i++
			}
			continue
		}
		i++
	}
	if i < n && data[i] == ']' {
		i++
	}
	return i
}

func assignPlayerField(p *FastPlayer, idx int, val []byte) {
	switch idx {
	case 0:
		p.Slot = int(parseIntBytes(val))
	case 4:
		p.Combo = int(parseFloatBytes(val))
	case 6:
		p.HP = parseFloatBytes(val)
	case 11:
		val = bytes.TrimSpace(val)
		p.IsDead = len(val) > 0 && val[0] == 't'
	}
}

func (s *SnapshotScanner) parseEvents(data []byte, start int) int {
	i := start + 1
	n := len(data)
	for i < n && data[i] != ']' {
		for i < n && (data[i] == ' ' || data[i] == ',' || data[i] == '\t' || data[i] == '\r' || data[i] == '\n') {
			i++
		}
		if i >= n || data[i] == ']' {
			break
		}
		if data[i] == '{' {
			objStart := i
			i = skipValue(data, i)
			s.RawEvents = append(s.RawEvents, data[objStart:i])
			continue
		}
		i++
	}
	if i < n && data[i] == ']' {
		i++
	}
	return i
}
