package toml //import "github.com/getsops/sops/v3/stores/toml"

import (
	"bytes"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/stores"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/pelletier/go-toml/v2/unstable"
)

// Store handles storage of TOML data.
type Store struct {
	config config.TOMLStoreConfig
}

func NewStore(c *config.TOMLStoreConfig) *Store {
	return &Store{config: *c}
}

func (store *Store) Name() string {
	return "toml"
}

// ---------------------------------------------------------------------------
// LoadPlainFile
// ---------------------------------------------------------------------------

func (store *Store) LoadPlainFile(in []byte) (sops.TreeBranches, error) {
	branch, err := parseTOML(in)
	if err != nil {
		return nil, fmt.Errorf("could not unmarshal TOML data: %w", err)
	}
	return sops.TreeBranches{branch}, nil
}

// branchRef is a mutable tree node used during parsing. It is converted to
// sops.TreeBranch after parsing is complete via finalize().
type branchRef struct {
	items []sops.TreeItem
}

type parseState struct {
	root  *branchRef
	scope *branchRef
}

func newParseState() *parseState {
	root := &branchRef{}
	return &parseState{root: root, scope: root}
}

func (ps *parseState) finalize() sops.TreeBranch {
	return finalizeBranch(ps.root)
}

func finalizeBranch(br *branchRef) sops.TreeBranch {
	if br == nil || len(br.items) == 0 {
		return nil
	}
	result := make(sops.TreeBranch, len(br.items))
	for i, item := range br.items {
		result[i] = item
		switch v := item.Value.(type) {
		case *branchRef:
			result[i].Value = finalizeBranch(v)
		case []any:
			result[i].Value = finalizeSlice(v)
		}
	}
	return result
}

func finalizeSlice(s []any) []any {
	result := make([]any, len(s))
	for i, v := range s {
		switch v := v.(type) {
		case *branchRef:
			result[i] = finalizeBranch(v)
		case []any:
			result[i] = finalizeSlice(v)
		default:
			result[i] = v
		}
	}
	return result
}

// navigateTo finds or creates a child branchRef for the given key.
func (ps *parseState) navigateTo(br *branchRef, key string) *branchRef {
	for i := len(br.items) - 1; i >= 0; i-- {
		if br.items[i].Key == key {
			switch v := br.items[i].Value.(type) {
			case *branchRef:
				return v
			case []any:
				if len(v) > 0 {
					if last, ok := v[len(v)-1].(*branchRef); ok {
						return last
					}
				}
			}
			break
		}
	}
	child := &branchRef{}
	br.items = append(br.items, sops.TreeItem{Key: key, Value: child})
	return child
}

// navigateForSection is like navigateTo but creates a new entry when the
// existing one is followed by other table-valued items. This preserves the
// original ordering of interleaved sections (e.g. [a.b] ... [c] ... [a.d]).
func (ps *parseState) navigateForSection(br *branchRef, key string) *branchRef {
	for i := len(br.items) - 1; i >= 0; i-- {
		if br.items[i].Key == key {
			// Check if any table-valued items were added after this entry.
			hasLaterSections := false
			for j := i + 1; j < len(br.items); j++ {
				if _, ok := br.items[j].Key.(sops.Comment); ok {
					continue
				}
				switch br.items[j].Value.(type) {
				case *branchRef, []any:
					hasLaterSections = true
				}
				if hasLaterSections {
					break
				}
			}
			if !hasLaterSections {
				switch v := br.items[i].Value.(type) {
				case *branchRef:
					return v
				case []any:
					if len(v) > 0 {
						if last, ok := v[len(v)-1].(*branchRef); ok {
							return last
						}
					}
				}
			}
			break
		}
	}
	child := &branchRef{}
	br.items = append(br.items, sops.TreeItem{Key: key, Value: child})
	return child
}

func (ps *parseState) setScope(path []string) {
	cur := ps.root
	for _, key := range path {
		cur = ps.navigateForSection(cur, key)
	}
	ps.scope = cur
}

func (ps *parseState) setScopeArrayTable(path []string) {
	cur := ps.root
	for i, key := range path {
		if i < len(path)-1 {
			cur = ps.navigateForSection(cur, key)
		} else {
			found := false
			for j := len(cur.items) - 1; j >= 0; j-- {
				if cur.items[j].Key == key {
					if arr, ok := cur.items[j].Value.([]any); ok {
						newBr := &branchRef{}
						cur.items[j].Value = append(arr, newBr)
						ps.scope = newBr
						found = true
					}
					break
				}
			}
			if !found {
				newBr := &branchRef{}
				cur.items = append(cur.items, sops.TreeItem{
					Key:   key,
					Value: []any{newBr},
				})
				ps.scope = newBr
			}
		}
	}
}

func (ps *parseState) addToScope(item sops.TreeItem) {
	ps.scope.items = append(ps.scope.items, item)
}

// addKVToNestedScope navigates from scope through parentPath, appends item, and
// returns the target branchRef (for attaching trailer comments).
func (ps *parseState) addKVToNestedScope(parentPath []string, item sops.TreeItem) *branchRef {
	target := ps.scope
	for _, key := range parentPath {
		target = ps.navigateTo(target, key)
	}
	target.items = append(target.items, item)
	return target
}

func parseTOML(data []byte) (sops.TreeBranch, error) {
	var p unstable.Parser
	p.KeepComments = true
	p.Reset(data)

	ps := newParseState()

	// Buffer comments so we can decide which scope they belong to.
	// If the next non-comment expression is a Table/ArrayTable, the buffered
	// comments go to the parent scope of the new section (the scope that
	// contains the new section entry).
	var pendingComments []sops.Comment

	flushComments := func(target *branchRef) {
		for _, c := range pendingComments {
			target.items = append(target.items, sops.TreeItem{Key: c, Value: nil})
		}
		pendingComments = pendingComments[:0]
	}

	// parentScopeForPath returns the branchRef that would contain the section
	// entry for the given path. For path ["a","b","c"], this navigates to "a">"b".
	// Uses navigateForSection so it agrees with setScope/setScopeArrayTable
	// about which branchRef to use when duplicate entries exist.
	parentScopeForPath := func(path []string) *branchRef {
		cur := ps.root
		for _, key := range path[:len(path)-1] {
			cur = ps.navigateForSection(cur, key)
		}
		return cur
	}

	for p.NextExpression() {
		node := p.Expression()
		switch node.Kind {
		case unstable.Comment:
			pendingComments = append(pendingComments, sops.Comment{Value: cleanComment(node.Data)})
		case unstable.KeyValue:
			// Comments before a KV go to the current scope
			flushComments(ps.scope)
			keyPath := extractKeyPath(node)
			val, err := nodeToGoValue(node.Value())
			if err != nil {
				return nil, err
			}
			target := ps.addKVToNestedScope(keyPath[:len(keyPath)-1], sops.TreeItem{
				Key:   keyPath[len(keyPath)-1],
				Value: val,
			})
			if c := node.Comment(); c != nil {
				target.items = append(target.items, sops.TreeItem{
					Key:   sops.Comment{Value: cleanComment(c.Data), Trailer: true},
					Value: nil,
				})
			}
		case unstable.Table:
			keyPath := extractKeyPath(node)
			flushComments(parentScopeForPath(keyPath))
			ps.setScope(keyPath)
			if c := node.Comment(); c != nil {
				ps.addToScope(sops.TreeItem{
					Key:   sops.Comment{Value: cleanComment(c.Data), Trailer: true},
					Value: nil,
				})
			}
		case unstable.ArrayTable:
			keyPath := extractKeyPath(node)
			flushComments(parentScopeForPath(keyPath))
			ps.setScopeArrayTable(keyPath)
			if c := node.Comment(); c != nil {
				ps.addToScope(sops.TreeItem{
					Key:   sops.Comment{Value: cleanComment(c.Data), Trailer: true},
					Value: nil,
				})
			}
		}
	}
	// Flush remaining comments to current scope
	flushComments(ps.scope)

	if err := p.Error(); err != nil {
		return nil, err
	}
	return ps.finalize(), nil
}

// extractKeyPath collects the dotted key parts from a KeyValue/Table/ArrayTable node.
func extractKeyPath(node *unstable.Node) []string {
	var parts []string
	it := node.Key()
	for it.Next() {
		parts = append(parts, string(it.Node().Data))
	}
	return parts
}

// cleanComment strips the leading "# " or "#" prefix from comment data.
func cleanComment(data []byte) string {
	s := string(data)
	if strings.HasPrefix(s, "# ") {
		return s[2:]
	}
	if strings.HasPrefix(s, "#") {
		return s[1:]
	}
	return s
}

// ---------------------------------------------------------------------------
// Value conversion
// ---------------------------------------------------------------------------

func nodeToGoValue(node *unstable.Node) (any, error) {
	switch node.Kind {
	case unstable.String:
		return string(node.Data), nil

	case unstable.Integer:
		s := strings.ReplaceAll(string(node.Data), "_", "")
		v, err := strconv.ParseInt(s, 0, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid integer %q: %w", node.Data, err)
		}
		return v, nil

	case unstable.Float:
		s := string(node.Data)
		switch s {
		case "inf", "+inf":
			return math.Inf(1), nil
		case "-inf":
			return math.Inf(-1), nil
		case "nan", "+nan":
			return math.NaN(), nil
		case "-nan":
			return math.NaN(), nil
		}
		s = strings.ReplaceAll(s, "_", "")
		v, err := strconv.ParseFloat(s, 64)
		if err != nil {
			return nil, fmt.Errorf("invalid float %q: %w", node.Data, err)
		}
		return v, nil

	case unstable.Bool:
		return string(node.Data) == "true", nil

	case unstable.LocalDate:
		var d toml.LocalDate
		if err := d.UnmarshalText(node.Data); err != nil {
			return nil, fmt.Errorf("invalid local date %q: %w", node.Data, err)
		}
		return d, nil

	case unstable.LocalTime:
		var t toml.LocalTime
		if err := t.UnmarshalText(node.Data); err != nil {
			return nil, fmt.Errorf("invalid local time %q: %w", node.Data, err)
		}
		return t, nil

	case unstable.LocalDateTime:
		var dt toml.LocalDateTime
		if err := dt.UnmarshalText(node.Data); err != nil {
			return nil, fmt.Errorf("invalid local datetime %q: %w", node.Data, err)
		}
		return dt, nil

	case unstable.DateTime:
		s := string(node.Data)
		s = strings.Replace(s, " ", "T", 1)
		for _, layout := range []string{
			time.RFC3339Nano,
			time.RFC3339,
		} {
			if t, err := time.Parse(layout, s); err == nil {
				return t, nil
			}
		}
		return nil, fmt.Errorf("invalid datetime %q", node.Data)

	case unstable.Array:
		return nodeToArray(node)

	case unstable.InlineTable:
		return nodeToInlineTable(node)

	default:
		return nil, fmt.Errorf("unsupported node kind: %s", node.Kind)
	}
}

func nodeToArray(node *unstable.Node) ([]any, error) {
	var result []any
	if c := node.Comment(); c != nil {
		result = append(result, sops.Comment{Value: cleanComment(c.Data), Trailer: true})
	}
	it := node.Children()
	for it.Next() {
		child := it.Node()
		if child.Kind == unstable.Comment {
			result = append(result, sops.Comment{Value: cleanComment(child.Data)})
			continue
		}
		val, err := nodeToGoValue(child)
		if err != nil {
			return nil, err
		}
		result = append(result, val)
		if c := child.Comment(); c != nil {
			result = append(result, sops.Comment{Value: cleanComment(c.Data), Trailer: true})
		}
	}
	return result, nil
}

func nodeToInlineTable(node *unstable.Node) (sops.TreeBranch, error) {
	var branch sops.TreeBranch
	if c := node.Comment(); c != nil {
		branch = append(branch, sops.TreeItem{
			Key:   sops.Comment{Value: cleanComment(c.Data), Trailer: true},
			Value: nil,
		})
	}
	it := node.Children()
	for it.Next() {
		child := it.Node()
		if child.Kind == unstable.Comment {
			branch = append(branch, sops.TreeItem{
				Key:   sops.Comment{Value: cleanComment(child.Data)},
				Value: nil,
			})
			continue
		}
		if child.Kind == unstable.KeyValue {
			keyPath := extractKeyPath(child)
			val, err := nodeToGoValue(child.Value())
			if err != nil {
				return nil, err
			}
			key := strings.Join(keyPath, ".")
			branch = append(branch, sops.TreeItem{Key: key, Value: val})
			if c := child.Comment(); c != nil {
				branch = append(branch, sops.TreeItem{
					Key:   sops.Comment{Value: cleanComment(c.Data), Trailer: true},
					Value: nil,
				})
			}
		}
	}
	return branch, nil
}

// ---------------------------------------------------------------------------
// EmitPlainFile
// ---------------------------------------------------------------------------

func (store *Store) EmitPlainFile(in sops.TreeBranches) ([]byte, error) {
	switch len(in) {
	case 0:
		return []byte{}, nil
	case 1:
	// Emit it. See below.
	default:
		return nil, errors.New("TOML files can only contain one document")
	}

	var buf bytes.Buffer
	pr := unstable.NewPrinter(&buf)
	b := unstable.NewBuilder()
	emitted := make(map[string]bool)
	for _, branch := range in {
		if err := emitScope(branch, nil, false, b, pr, &buf, emitted); err != nil {
			return nil, err
		}
	}

	out := buf.Bytes()
	if len(out) > 0 && !bytes.HasSuffix(out, []byte{'\n'}) {
		out = append(out, '\n')
	}
	return out, nil
}

// isAllTreeBranch returns true if every non-Comment element in s is a TreeBranch.
func isAllTreeBranch(s []any) bool {
	count := 0
	for _, v := range s {
		if _, ok := v.(sops.Comment); ok {
			continue
		}
		if _, ok := v.(sops.TreeBranch); !ok {
			return false
		}
		count++
	}
	return count > 0
}

func isTableValue(v any) bool {
	switch v := v.(type) {
	case sops.TreeBranch:
		return true
	case []any:
		return isAllTreeBranch(v)
	}
	return false
}

type groupedItem struct {
	item    sops.TreeItem
	isTable bool
}

func emitScope(branch sops.TreeBranch, path []string, isArrayTable bool, b *unstable.Builder, pr *unstable.Printer, w *bytes.Buffer, emitted map[string]bool) error {
	startIdx := 0

	// Emit section header (skip if this non-array-table path was already emitted)
	if len(path) > 0 {
		pathKey := strings.Join(path, "\x00")
		if !isArrayTable && emitted[pathKey] {
			// Header already emitted by an earlier duplicate entry; skip it.
		} else {
			emitted[pathKey] = true
			b.Reset()
			firstKey, lastKey := unstable.InvalidReference, unstable.InvalidReference
			for _, p := range path {
				kRef := b.Push(unstable.Node{Kind: unstable.Key, Data: []byte(p)})
				if !firstKey.Valid() {
					firstKey = kRef
				} else {
					b.Chain(lastKey, kRef)
				}
				lastKey = kRef
			}
			headerKind := unstable.Table
			if isArrayTable {
				headerKind = unstable.ArrayTable
			}
			headerRef := b.Push(unstable.Node{Kind: headerKind})
			b.AttachChild(headerRef, firstKey)
			if len(branch) > 0 {
				if c, ok := branch[0].Key.(sops.Comment); ok && c.Trailer {
					cRef := b.Push(unstable.Node{Kind: unstable.Comment, Data: []byte("# " + c.Value)})
					b.AttachComment(headerRef, cRef)
					startIdx = 1
				}
			}
			if err := pr.Format(b.NodeAt(headerRef)); err != nil {
				return err
			}
		}
	}

	items := branch[startIdx:]

	// First pass: tag each item
	grouped := make([]groupedItem, len(items))
	for i, item := range items {
		if _, ok := item.Key.(sops.Comment); ok {
			grouped[i] = groupedItem{item: item}
		} else {
			grouped[i] = groupedItem{item: item, isTable: isTableValue(item.Value)}
		}
	}

	// Second pass: associate comments with the next non-comment item's group.
	for i := len(grouped) - 1; i >= 0; i-- {
		if _, ok := grouped[i].item.Key.(sops.Comment); ok {
			for j := i + 1; j < len(grouped); j++ {
				if _, ok := grouped[j].item.Key.(sops.Comment); !ok {
					grouped[i].isTable = grouped[j].isTable
					break
				}
			}
		}
	}

	// Emit KV items.
	for i := 0; i < len(grouped); i++ {
		g := grouped[i]
		if g.isTable {
			continue
		}
		item := g.item
		if c, ok := item.Key.(sops.Comment); ok {
			b.Reset()
			cRef := b.Push(unstable.Node{Kind: unstable.Comment, Data: []byte("# " + c.Value)})
			if err := pr.Format(b.NodeAt(cRef)); err != nil {
				return err
			}
		} else {
			b.Reset()
			key := item.Key.(string)
			valRef, err := buildValueNode(b, item.Value)
			if err != nil {
				return err
			}
			keyRef := b.Push(unstable.Node{Kind: unstable.Key, Data: []byte(key)})
			b.Chain(valRef, keyRef)
			kvRef := b.Push(unstable.Node{Kind: unstable.KeyValue})
			b.AttachChild(kvRef, valRef)
			// Peek ahead: if the next non-table item is a trailer
			// comment, attach it to this KV node so the printer
			// emits it inline.
			if i+1 < len(grouped) && !grouped[i+1].isTable {
				if nc, ok := grouped[i+1].item.Key.(sops.Comment); ok && nc.Trailer {
					cRef := b.Push(unstable.Node{Kind: unstable.Comment, Data: []byte("# " + nc.Value)})
					b.AttachComment(kvRef, cRef)
					i++ // consume the trailer
				}
			}
			if err := pr.Format(b.NodeAt(kvRef)); err != nil {
				return err
			}
		}
	}

	// Emit table items. Collect pending comments to emit immediately before
	// their associated section header (no extra blank line between comment
	// and header).
	var pendingComments []sops.Comment
	for _, g := range grouped {
		if !g.isTable {
			continue
		}
		item := g.item
		if c, ok := item.Key.(sops.Comment); ok {
			pendingComments = append(pendingComments, c)
			continue
		}
		key := item.Key.(string)
		newPath := append(append([]string{}, path...), key)
		switch v := item.Value.(type) {
		case sops.TreeBranch:
			pathKey := strings.Join(newPath, "\x00")
			if emitted[pathKey] {
				// Duplicate entry: skip blank line and pending comments,
				// but still recurse to emit new children.
				if err := emitScope(v, newPath, false, b, pr, w, emitted); err != nil {
					return err
				}
			} else {
				w.WriteByte('\n')
				if err := emitPendingComments(&pendingComments, b, pr); err != nil {
					return err
				}
				if err := emitScope(v, newPath, false, b, pr, w, emitted); err != nil {
					return err
				}
			}
		case []any:
			if isAllTreeBranch(v) {
				for _, elem := range v {
					if _, ok := elem.(sops.Comment); ok {
						continue
					}
					br := elem.(sops.TreeBranch)
					w.WriteByte('\n')
					if err := emitPendingComments(&pendingComments, b, pr); err != nil {
						return err
					}
					if err := emitScope(br, newPath, true, b, pr, w, emitted); err != nil {
						return err
					}
				}
			}
		}
	}
	// Any remaining pending comments (trailing comments after last table)
	if len(pendingComments) > 0 {
		if err := emitPendingComments(&pendingComments, b, pr); err != nil {
			return err
		}
	}

	return nil
}

func emitPendingComments(pending *[]sops.Comment, b *unstable.Builder, pr *unstable.Printer) error {
	for _, c := range *pending {
		b.Reset()
		cRef := b.Push(unstable.Node{Kind: unstable.Comment, Data: []byte("# " + c.Value)})
		if err := pr.Format(b.NodeAt(cRef)); err != nil {
			return err
		}
	}
	*pending = (*pending)[:0]
	return nil
}

// ---------------------------------------------------------------------------
// AST node building
// ---------------------------------------------------------------------------

func buildValueNode(b *unstable.Builder, v any) (unstable.Reference, error) {
	switch v := v.(type) {
	case string:
		return b.Push(unstable.Node{Kind: unstable.String, Data: []byte(v)}), nil
	case int64:
		return b.Push(unstable.Node{Kind: unstable.Integer, Data: []byte(strconv.FormatInt(v, 10))}), nil
	case int:
		return b.Push(unstable.Node{Kind: unstable.Integer, Data: []byte(strconv.Itoa(v))}), nil
	case float64:
		return b.Push(unstable.Node{Kind: unstable.Float, Data: []byte(formatFloat(v))}), nil
	case bool:
		if v {
			return b.Push(unstable.Node{Kind: unstable.Bool, Data: []byte("true")}), nil
		}
		return b.Push(unstable.Node{Kind: unstable.Bool, Data: []byte("false")}), nil
	case toml.LocalDate:
		return b.Push(unstable.Node{Kind: unstable.LocalDate, Data: []byte(v.String())}), nil
	case toml.LocalTime:
		return b.Push(unstable.Node{Kind: unstable.LocalTime, Data: []byte(v.String())}), nil
	case toml.LocalDateTime:
		return b.Push(unstable.Node{Kind: unstable.LocalDateTime, Data: []byte(v.String())}), nil
	case time.Time:
		return b.Push(unstable.Node{Kind: unstable.DateTime, Data: []byte(v.Format("2006-01-02T15:04:05.999999999Z07:00"))}), nil
	case []any:
		return buildArrayNode(b, v)
	case sops.TreeBranch:
		return buildInlineTableNode(b, v)
	default:
		return b.Push(unstable.Node{Kind: unstable.String, Data: []byte(fmt.Sprintf("%v", v))}), nil
	}
}

func buildArrayNode(b *unstable.Builder, arr []any) (unstable.Reference, error) {
	var firstChild, lastChild unstable.Reference = unstable.InvalidReference, unstable.InvalidReference
	var lastValueRef unstable.Reference = unstable.InvalidReference
	var bracketComment unstable.Reference = unstable.InvalidReference

	for i, elem := range arr {
		if c, ok := elem.(sops.Comment); ok {
			if c.Trailer {
				if i == 0 {
					// Bracket trailer (e.g., "[ # header")
					bracketComment = b.Push(unstable.Node{Kind: unstable.Comment, Data: []byte("# " + c.Value)})
				} else if lastValueRef.Valid() {
					// Value trailer
					cRef := b.Push(unstable.Node{Kind: unstable.Comment, Data: []byte("# " + c.Value)})
					b.AttachComment(lastValueRef, cRef)
					lastValueRef = unstable.InvalidReference
				}
			} else {
				cRef := b.Push(unstable.Node{Kind: unstable.Comment, Data: []byte("# " + c.Value)})
				if !firstChild.Valid() {
					firstChild = cRef
				} else {
					b.Chain(lastChild, cRef)
				}
				lastChild = cRef
				lastValueRef = unstable.InvalidReference
			}
		} else {
			vRef, err := buildValueNode(b, elem)
			if err != nil {
				return unstable.InvalidReference, err
			}
			if !firstChild.Valid() {
				firstChild = vRef
			} else {
				b.Chain(lastChild, vRef)
			}
			lastChild = vRef
			lastValueRef = vRef
		}
	}

	arrRef := b.Push(unstable.Node{Kind: unstable.Array})
	if firstChild.Valid() {
		b.AttachChild(arrRef, firstChild)
	}
	if bracketComment.Valid() {
		b.AttachComment(arrRef, bracketComment)
	}

	return arrRef, nil
}

func buildInlineTableNode(b *unstable.Builder, branch sops.TreeBranch) (unstable.Reference, error) {
	var firstChild, lastChild unstable.Reference = unstable.InvalidReference, unstable.InvalidReference
	var lastKVRef unstable.Reference = unstable.InvalidReference
	var bracketComment unstable.Reference = unstable.InvalidReference

	for i, item := range branch {
		if c, ok := item.Key.(sops.Comment); ok {
			if c.Trailer {
				if i == 0 {
					bracketComment = b.Push(unstable.Node{Kind: unstable.Comment, Data: []byte("# " + c.Value)})
				} else if lastKVRef.Valid() {
					cRef := b.Push(unstable.Node{Kind: unstable.Comment, Data: []byte("# " + c.Value)})
					b.AttachComment(lastKVRef, cRef)
					lastKVRef = unstable.InvalidReference
				}
			} else {
				cRef := b.Push(unstable.Node{Kind: unstable.Comment, Data: []byte("# " + c.Value)})
				if !firstChild.Valid() {
					firstChild = cRef
				} else {
					b.Chain(lastChild, cRef)
				}
				lastChild = cRef
				lastKVRef = unstable.InvalidReference
			}
		} else {
			key := item.Key.(string)
			vRef, err := buildValueNode(b, item.Value)
			if err != nil {
				return unstable.InvalidReference, err
			}
			keyRef := b.Push(unstable.Node{Kind: unstable.Key, Data: []byte(key)})
			b.Chain(vRef, keyRef)
			kvRef := b.Push(unstable.Node{Kind: unstable.KeyValue})
			b.AttachChild(kvRef, vRef)

			if !firstChild.Valid() {
				firstChild = kvRef
			} else {
				b.Chain(lastChild, kvRef)
			}
			lastChild = kvRef
			lastKVRef = kvRef
		}
	}

	tblRef := b.Push(unstable.Node{Kind: unstable.InlineTable})
	if firstChild.Valid() {
		b.AttachChild(tblRef, firstChild)
	}
	if bracketComment.Valid() {
		b.AttachComment(tblRef, bracketComment)
	}

	return tblRef, nil
}

func formatFloat(v float64) string {
	if math.IsInf(v, 1) {
		return "inf"
	} else if math.IsInf(v, -1) {
		return "-inf"
	} else if math.IsNaN(v) {
		return "nan"
	}
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}

// ---------------------------------------------------------------------------
// LoadEncryptedFile
// ---------------------------------------------------------------------------

func (store *Store) LoadEncryptedFile(in []byte) (sops.Tree, error) {
	var sopsFile stores.SopsFile
	if err := toml.Unmarshal(in, &sopsFile); err != nil {
		return sops.Tree{}, fmt.Errorf("could not unmarshal TOML metadata: %w", err)
	}
	if sopsFile.Metadata == nil {
		return sops.Tree{}, sops.MetadataNotFound
	}
	metadata, err := sopsFile.Metadata.ToInternal()
	if err != nil {
		return sops.Tree{}, err
	}

	branches, err := store.LoadPlainFile(in)
	if err != nil {
		return sops.Tree{}, fmt.Errorf("could not unmarshal input data: %w", err)
	}

	for bi, branch := range branches {
		for i, item := range branch {
			if item.Key == stores.SopsMetadataKey {
				branches[bi] = append(branch[:i], branch[i+1:]...)
				break
			}
		}
	}

	return sops.Tree{
		Branches: branches,
		Metadata: metadata,
	}, nil
}

// ---------------------------------------------------------------------------
// EmitEncryptedFile
// ---------------------------------------------------------------------------

func (store *Store) EmitEncryptedFile(in sops.Tree) ([]byte, error) {
	dataBytes, err := store.EmitPlainFile(in.Branches)
	if err != nil {
		return nil, fmt.Errorf("could not marshal TOML data: %w", err)
	}

	metadata := stores.MetadataFromInternal(in.Metadata)
	mdBytes, err := toml.Marshal(map[string]any{stores.SopsMetadataKey: metadata})
	if err != nil {
		return nil, fmt.Errorf("could not marshal TOML metadata: %w", err)
	}

	dataBytes = append(dataBytes, mdBytes...)
	return dataBytes, nil
}

// ---------------------------------------------------------------------------
// EmitValue
// ---------------------------------------------------------------------------

func (store *Store) EmitValue(v interface{}) ([]byte, error) {
	switch v := v.(type) {
	case sops.TreeBranches:
		return store.EmitPlainFile(v)
	default:
		return toml.Marshal(v)
	}
}

// ---------------------------------------------------------------------------
// EmitExample
// ---------------------------------------------------------------------------

func (store *Store) EmitExample() []byte {
	bytes, err := store.EmitPlainFile(stores.ExampleSimpleTree.Branches)
	if err != nil {
		panic(err)
	}
	return bytes
}

// ---------------------------------------------------------------------------
// HasSopsTopLevelKey
// ---------------------------------------------------------------------------

func (store *Store) HasSopsTopLevelKey(branch sops.TreeBranch) bool {
	return stores.HasSopsTopLevelKey(branch)
}
