package conffile

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"unicode/utf8"

	yamlv2 "gopkg.in/yaml.v2"
	yaml "gopkg.in/yaml.v3"
)

// doc is the config file's text as lines. Edits change lines; the YAML is
// parsed again after each one only to find where things are.
type doc struct {
	lines   []string
	newline bool
}

var errMultiline = errors.New("values written across several lines can't be edited here")

func parse(dat []byte) *doc {
	text := string(dat)
	d := &doc{newline: strings.HasSuffix(text, "\n")}
	text = strings.TrimSuffix(text, "\n")
	if text != "" {
		d.lines = strings.Split(text, "\n")
	}
	return d
}

func (d *doc) bytes() []byte {
	out := strings.Join(d.lines, "\n")
	if d.newline && len(d.lines) > 0 {
		out += "\n"
	}
	return []byte(out)
}

// root is the file's top-level mapping, or nil for a file with nothing in it.
func (d *doc) root() (*yaml.Node, error) {
	var n yaml.Node
	if err := yaml.Unmarshal(d.bytes(), &n); err != nil {
		return nil, err
	}
	if len(n.Content) == 0 {
		return nil, nil
	}

	root := n.Content[0]
	if root.Kind != yaml.MappingNode || root.Style&yaml.FlowStyle != 0 {
		return nil, errors.New("the config file is not a list of sections")
	}

	return root, nil
}

func (d *doc) topKeys() ([]string, error) {
	root, err := d.root()
	if err != nil || root == nil {
		return nil, err
	}

	keys := make([]string, 0, len(root.Content)/2)
	for i := 0; i+1 < len(root.Content); i += 2 {
		keys = append(keys, root.Content[i].Value)
	}

	return keys, nil
}

func lookup(m *yaml.Node, key string) (*yaml.Node, *yaml.Node) {
	for i := 0; i+1 < len(m.Content); i += 2 {
		if strings.EqualFold(m.Content[i].Value, key) {
			return m.Content[i], m.Content[i+1]
		}
	}
	return nil, nil
}

// hasValue reports whether the file already says value at path, as the
// config loader would read it.
func (d *doc) hasValue(path []string, value any) (bool, error) {
	cfg, err := decode(d.bytes())
	if err != nil {
		return false, err
	}

	got, ok := getPath(cfg, path)
	return ok && reflect.DeepEqual(got, value), nil
}

func (d *doc) set(path []string, value any) error {
	root, err := d.root()
	if err != nil {
		return err
	}

	if root == nil {
		lines, err := renderPath(path, value, 0)
		if err != nil {
			return err
		}
		d.appendSection(lines)
		return nil
	}

	parent := root
	for i, key := range path {
		k, v := lookup(parent, key)

		if k == nil {
			if parent == root {
				lines, err := renderPath(path[i:], value, 0)
				if err != nil {
					return err
				}
				d.appendSection(lines)
				return nil
			}

			// a new key goes at the end of its section, at the indent the
			// section's keys already use
			last, err := lastLine(parent)
			if err != nil {
				return err
			}
			lines, err := renderPath(path[i:], value, parent.Content[0].Column-1)
			if err != nil {
				return err
			}
			d.insertAfter(last, lines)
			return nil
		}

		if i == len(path)-1 {
			if list, ok := value.([]string); ok {
				return d.replaceList(k, v, list)
			}
			return d.replaceScalar(k, v, value)
		}

		switch {
		case v.Kind == yaml.MappingNode && v.Style&yaml.FlowStyle == 0:
			parent = v
		case isImplicitNull(v):
			// "stats:" with nothing under it yet
			lines, err := renderPath(path[i+1:], value, k.Column-1+2)
			if err != nil {
				return err
			}
			d.insertAfter(k.Line, lines)
			return nil
		default:
			return fmt.Errorf("%s is a value, not a section", k.Value)
		}
	}

	return nil
}

func (d *doc) replaceScalar(k, v *yaml.Node, value any) error {
	if v.Kind != yaml.ScalarNode {
		return fmt.Errorf("%s holds more than one value", k.Value)
	}

	// only a string keeps the quoting it had
	style := yaml.Style(0)
	if v.Tag == "!!str" {
		style = v.Style
	}

	text, err := renderScalar(value, style)
	if err != nil {
		return err
	}

	if isImplicitNull(v) {
		return d.afterColon(k, " "+text)
	}

	return d.replaceToken(v.Line, v.Column, text)
}

func (d *doc) replaceList(k, v *yaml.Node, items []string) error {
	switch {
	case v.Kind == yaml.SequenceNode && v.Style&yaml.FlowStyle != 0:
		last, err := lastLine(v)
		if err != nil {
			return err
		}
		if last != v.Line {
			return errMultiline
		}
		return d.replaceFlow(v, renderFlowList(items, itemStyle(v)))

	case v.Kind == yaml.SequenceNode:
		last, err := lastLine(v)
		if err != nil {
			return err
		}
		if len(items) == 0 {
			d.splice(v.Line-1, last, nil)
			return d.afterColon(k, " []")
		}
		lines, err := renderItems(items, v.Column-1, itemStyle(v))
		if err != nil {
			return err
		}
		d.splice(v.Line-1, last, lines)
		return nil

	case v.Kind == yaml.ScalarNode:
		if !isImplicitNull(v) {
			// "watchTeams: ALL" becomes a list under the key
			if v.Line != k.Line {
				return errMultiline
			}
			if err := d.replaceToken(v.Line, v.Column, ""); err != nil {
				return err
			}
			i := k.Line - 1
			d.lines[i] = strings.TrimRight(d.lines[i], " \t")
		}
		if len(items) == 0 {
			return d.afterColon(k, " []")
		}
		lines, err := renderItems(items, k.Column-1, 0)
		if err != nil {
			return err
		}
		d.insertAfter(k.Line, lines)
		return nil

	default:
		return fmt.Errorf("%s is a section, not a list", k.Value)
	}
}

// replaceToken replaces the single-line scalar starting at line and column
// (both counted from 1, as yaml.v3 reports them) with text, leaving anything
// after it on the line -- a comment, say -- alone.
func (d *doc) replaceToken(line, column int, text string) error {
	i := line - 1
	s := d.lines[i]
	start := byteOffset(s, column-1)
	end, err := tokenEnd(s, start)
	if err != nil {
		return err
	}
	d.lines[i] = s[:start] + text + s[end:]
	return nil
}

func (d *doc) replaceFlow(v *yaml.Node, text string) error {
	i := v.Line - 1
	s := d.lines[i]
	start := byteOffset(s, v.Column-1)
	depth := 0
	for j := start; j < len(s); j++ {
		switch s[j] {
		case '[':
			depth++
		case ']':
			depth--
			if depth == 0 {
				d.lines[i] = s[:start] + text + s[j+1:]
				return nil
			}
		case '"', '\'':
			end, err := tokenEnd(s, j)
			if err != nil {
				return err
			}
			j = end - 1
		}
	}
	return errMultiline
}

// afterColon puts text straight after the colon that ends key k.
func (d *doc) afterColon(k *yaml.Node, text string) error {
	i := k.Line - 1
	s := d.lines[i]
	start := byteOffset(s, k.Column-1)

	end := start + len(k.Value)
	if k.Style&(yaml.DoubleQuotedStyle|yaml.SingleQuotedStyle) != 0 {
		var err error
		if end, err = tokenEnd(s, start); err != nil {
			return err
		}
	}

	colon := strings.IndexByte(s[end:], ':')
	if colon < 0 {
		return fmt.Errorf("could not find where %s ends", k.Value)
	}
	at := end + colon + 1
	d.lines[i] = s[:at] + text + s[at:]
	return nil
}

func (d *doc) splice(from, to int, lines []string) {
	out := make([]string, 0, len(d.lines)-(to-from)+len(lines))
	out = append(out, d.lines[:from]...)
	out = append(out, lines...)
	out = append(out, d.lines[to:]...)
	d.lines = out
}

// insertAfter inserts lines after line, counted from 1.
func (d *doc) insertAfter(line int, lines []string) {
	d.splice(line, line, lines)
}

// appendSection adds a new top-level section at the end of the file, set
// apart from the one before it the way the others are.
func (d *doc) appendSection(lines []string) {
	if n := len(d.lines); n > 0 && !isBlank(d.lines[n-1]) {
		d.lines = append(d.lines, "")
	}
	d.lines = append(d.lines, lines...)
	d.newline = true
}

// order rearranges top-level sections: see File.Order.
func (d *doc) order(keys []string) error {
	root, err := d.root()
	if err != nil || root == nil {
		return err
	}

	var (
		starts []int
		names  []string
	)
	floor := 0
	for i := 0; i+1 < len(root.Content); i += 2 {
		k := root.Content[i]
		if k.Column != 1 {
			return errors.New("top-level keys have to start their lines")
		}
		keyIdx := k.Line - 1
		starts = append(starts, attachStart(d.lines, keyIdx, floor))
		names = append(names, strings.ToLower(k.Value))
		floor = keyIdx + 1
	}
	if len(starts) == 0 {
		return nil
	}

	blocks := make([][]string, len(starts))
	for i := range starts {
		end := len(d.lines)
		if i+1 < len(starts) {
			end = starts[i+1]
		}
		blocks[i] = append([]string(nil), d.lines[starts[i]:end]...)
	}

	want := make(map[string]int, len(keys))
	for i, key := range keys {
		if _, dup := want[strings.ToLower(key)]; !dup {
			want[strings.ToLower(key)] = i
		}
	}

	var slots []int
	for i, name := range names {
		if _, ok := want[name]; ok {
			slots = append(slots, i)
		}
	}
	moving := append([]int(nil), slots...)
	sort.SliceStable(moving, func(a, b int) bool {
		return want[names[moving[a]]] < want[names[moving[b]]]
	})

	arranged := make([]int, len(blocks))
	for i := range arranged {
		arranged[i] = i
	}
	moved := false
	for j, slot := range slots {
		arranged[slot] = moving[j]
		moved = moved || moving[j] != slot
	}
	if !moved {
		return nil
	}

	out := append([]string(nil), d.lines[:starts[0]]...)
	for pos, bi := range arranged {
		b := blocks[bi]
		if pos < len(arranged)-1 {
			// keep sections apart, including the one that used to end the file
			if n := len(b); n > 0 && !isBlank(b[n-1]) {
				b = append(b, "")
			}
		} else {
			for len(b) > 0 && isBlank(b[len(b)-1]) {
				b = b[:len(b)-1]
			}
		}
		out = append(out, b...)
	}

	d.lines = out
	return nil
}

// attachStart finds where the section whose key is on line keyIdx starts: the
// comment lines directly above the key are its heading. A blank line ends the
// heading. If the comments instead run straight into the previous section's
// last value, only the unindented ones next to the key are this section's --
// the indented ones are the previous section's commented-out options.
func attachStart(lines []string, keyIdx, floor int) int {
	i := keyIdx
	for i > floor && isComment(lines[i-1]) {
		i--
	}
	if i == floor || isBlank(lines[i-1]) {
		return i
	}

	j := keyIdx
	for j > i && isComment(lines[j-1]) && !strings.HasPrefix(lines[j-1], " ") && !strings.HasPrefix(lines[j-1], "\t") {
		j--
	}
	return j
}

func isBlank(s string) bool {
	return strings.TrimSpace(s) == ""
}

func isComment(s string) bool {
	return strings.HasPrefix(strings.TrimSpace(s), "#")
}

func isImplicitNull(n *yaml.Node) bool {
	return n.Kind == yaml.ScalarNode && n.Tag == "!!null" && n.Value == ""
}

// lastLine is the line, counted from 1, that node n ends on.
func lastLine(n *yaml.Node) (int, error) {
	if len(n.Content) > 0 {
		return lastLine(n.Content[len(n.Content)-1])
	}
	if n.Style&(yaml.LiteralStyle|yaml.FoldedStyle) != 0 || strings.Contains(n.Value, "\n") {
		return 0, errMultiline
	}
	return n.Line, nil
}

// byteOffset turns a column counted in characters into a byte offset.
func byteOffset(s string, col int) int {
	off := 0
	for i := 0; i < col && off < len(s); i++ {
		_, size := utf8.DecodeRuneInString(s[off:])
		off += size
	}
	return off
}

// tokenEnd is the byte offset just past the scalar that starts at start.
func tokenEnd(s string, start int) (int, error) {
	if start >= len(s) {
		return start, nil
	}

	switch s[start] {
	case '"':
		for i := start + 1; i < len(s); i++ {
			switch s[i] {
			case '\\':
				i++
			case '"':
				return i + 1, nil
			}
		}
		return 0, errMultiline
	case '\'':
		for i := start + 1; i < len(s); i++ {
			if s[i] == '\'' {
				if i+1 < len(s) && s[i+1] == '\'' {
					i++
					continue
				}
				return i + 1, nil
			}
		}
		return 0, errMultiline
	default:
		end := len(s)
		for i := start; i < len(s); i++ {
			if s[i] == '#' && i > start && (s[i-1] == ' ' || s[i-1] == '\t') {
				end = i
				break
			}
		}
		return start + len(strings.TrimRight(s[start:end], " \t\r")), nil
	}
}

func renderPath(path []string, value any, indent int) ([]string, error) {
	pad := strings.Repeat(" ", indent)
	key := path[0]

	if len(path) > 1 {
		rest, err := renderPath(path[1:], value, indent+2)
		if err != nil {
			return nil, err
		}
		return append([]string{pad + key + ":"}, rest...), nil
	}

	if list, ok := value.([]string); ok {
		if len(list) == 0 {
			return []string{pad + key + ": []"}, nil
		}
		items, err := renderItems(list, indent, 0)
		if err != nil {
			return nil, err
		}
		return append([]string{pad + key + ":"}, items...), nil
	}

	text, err := renderScalar(value, 0)
	if err != nil {
		return nil, err
	}
	return []string{pad + key + ": " + text}, nil
}

func renderItems(items []string, indent int, style yaml.Style) ([]string, error) {
	pad := strings.Repeat(" ", indent)
	lines := make([]string, 0, len(items))
	for _, item := range items {
		text, err := renderScalar(item, style)
		if err != nil {
			return nil, err
		}
		lines = append(lines, pad+"- "+text)
	}
	return lines, nil
}

var flowPlain = regexp.MustCompile(`^[A-Za-z0-9_.-]+$`)

func renderFlowList(items []string, style yaml.Style) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		// flow lists have more characters that need quoting; keep to the dull ones
		if style == 0 && flowPlain.MatchString(item) && plainSafe(item) {
			parts = append(parts, item)
			continue
		}
		parts = append(parts, doubleQuoted(item))
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func itemStyle(seq *yaml.Node) yaml.Style {
	if len(seq.Content) > 0 && seq.Content[0].Tag == "!!str" {
		return seq.Content[0].Style & (yaml.DoubleQuotedStyle | yaml.SingleQuotedStyle)
	}
	return 0
}

func renderScalar(value any, style yaml.Style) (string, error) {
	switch v := value.(type) {
	case bool:
		return strconv.FormatBool(v), nil
	case int:
		return strconv.Itoa(v), nil
	case string:
		switch {
		case style&yaml.DoubleQuotedStyle != 0:
			return doubleQuoted(v), nil
		case style&yaml.SingleQuotedStyle != 0 && !strings.ContainsAny(v, "\n\r"):
			return "'" + strings.ReplaceAll(v, "'", "''") + "'", nil
		case plainSafe(v):
			return v, nil
		default:
			return doubleQuoted(v), nil
		}
	default:
		return "", fmt.Errorf("can't write a %T to the config file", value)
	}
}

// plainSafe reports whether s can be written without quotes and still read
// back as the same string -- by the YAML 1.1 reader the config is loaded with,
// where NO, the New Orleans Saints, is false.
func plainSafe(s string) bool {
	if s == "" || strings.TrimSpace(s) != s || strings.ContainsAny(s, "\n\r\t") || strings.Contains(s, " #") {
		return false
	}

	text := []byte("v: " + s)

	var v2 map[string]any
	if err := yamlv2.Unmarshal(text, &v2); err != nil {
		return false
	}
	if got, ok := v2["v"].(string); !ok || got != s {
		return false
	}

	// and this package reads the file with yaml.v3 to find its way around
	var v3 map[string]any
	if err := yaml.Unmarshal(text, &v3); err != nil {
		return false
	}
	got, ok := v3["v"].(string)
	return ok && got == s
}

// doubleQuoted writes s as a JSON string, which is also a YAML double-quoted
// scalar, and always a single line.
func doubleQuoted(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(s)
	return strings.TrimSuffix(buf.String(), "\n")
}

// normalize puts a value the way decode reads one, to compare them.
func normalize(value any) (any, error) {
	switch v := value.(type) {
	case bool, int, string:
		return v, nil
	case []string:
		out := make([]any, len(v))
		for i, s := range v {
			out[i] = s
		}
		return out, nil
	default:
		return nil, fmt.Errorf("can't write a %T to the config file", value)
	}
}

// decode reads config text the way the config loader does, YAML 1.1 and all,
// into maps keyed by string.
func decode(dat []byte) (map[string]any, error) {
	var raw any
	if err := yamlv2.Unmarshal(dat, &raw); err != nil {
		return nil, err
	}
	if raw == nil {
		return map[string]any{}, nil
	}
	m, ok := stringKeys(raw).(map[string]any)
	if !ok {
		return nil, errors.New("the config file is not a list of sections")
	}
	return m, nil
}

func stringKeys(v any) any {
	switch t := v.(type) {
	case map[any]any:
		m := make(map[string]any, len(t))
		for k, val := range t {
			m[fmt.Sprint(k)] = stringKeys(val)
		}
		return m
	case []any:
		for i := range t {
			t[i] = stringKeys(t[i])
		}
		return t
	default:
		return v
	}
}

func matchKey(m map[string]any, key string) (string, bool) {
	if _, ok := m[key]; ok {
		return key, true
	}
	for k := range m {
		if strings.EqualFold(k, key) {
			return k, true
		}
	}
	return key, false
}

func getPath(m map[string]any, path []string) (any, bool) {
	var cur any = m
	for _, key := range path {
		mm, ok := cur.(map[string]any)
		if !ok {
			return nil, false
		}
		k, found := matchKey(mm, key)
		if !found {
			return nil, false
		}
		cur = mm[k]
	}
	return cur, true
}

func setPath(m map[string]any, path []string, value any) {
	cur := m
	for i, key := range path {
		k, _ := matchKey(cur, key)
		if i == len(path)-1 {
			cur[k] = value
			return
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			next = map[string]any{}
			cur[k] = next
		}
		cur = next
	}
}

func verifyEdits(old, out []byte, edits []Edit) error {
	want, err := decode(old)
	if err != nil {
		return fmt.Errorf("the config file doesn't read as it is: %w", err)
	}
	for _, e := range edits {
		value, err := normalize(e.Value)
		if err != nil {
			return err
		}
		setPath(want, e.Path, value)
	}

	got, err := decode(out)
	if err != nil {
		return fmt.Errorf("the edit would leave a config file that doesn't load, so it was not made: %w", err)
	}
	if !reflect.DeepEqual(want, got) {
		return errors.New("the edit would change more of the config file than it should, so it was not made")
	}

	return nil
}

func verifyOrder(old, out []byte, keys []string) error {
	before, err := decode(old)
	if err != nil {
		return err
	}
	after, err := decode(out)
	if err != nil {
		return fmt.Errorf("reordering would leave a config file that doesn't load, so it was not done: %w", err)
	}
	if !reflect.DeepEqual(before, after) {
		return errors.New("reordering would change what the config file says, so it was not done")
	}

	got, err := parse(out).topKeys()
	if err != nil {
		return err
	}
	want := make(map[string]int, len(keys))
	for i, k := range keys {
		if _, dup := want[strings.ToLower(k)]; !dup {
			want[strings.ToLower(k)] = i
		}
	}
	last := -1
	for _, k := range got {
		if i, ok := want[strings.ToLower(k)]; ok {
			if i < last {
				return errors.New("reordering didn't come out in the order asked for, so it was not done")
			}
			last = i
		}
	}

	return nil
}
