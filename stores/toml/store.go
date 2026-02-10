package toml //import "github.com/getsops/sops/v3/stores/toml"

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/creachadair/tomledit"
	"github.com/creachadair/tomledit/parser"
	"github.com/creachadair/tomledit/scanner"
	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/stores"
	toml "github.com/pelletier/go-toml/v2"
)

var ErrTOMLUniqueDocument = errors.New("TOML files can only contain one document")

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

// cleanCommentText strips the "# " or "#" prefix from raw comment text.
func (store *Store) cleanCommentText(raw string) string {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "# ") {
		return raw[2:]
	}
	if strings.HasPrefix(raw, "#") {
		return raw[1:]
	}
	return raw
}

// tokenToGoValue converts a parser.Token to a Go value.
func (store *Store) tokenToGoValue(tok parser.Token) (interface{}, error) {
	text := tok.String()
	switch tok.Type {
	case scanner.String, scanner.MString:
		// Strip surrounding quotes and unescape.
		stripped := store.stripStringQuotes(text)
		unescaped, err := scanner.Unescape([]byte(stripped))
		if err != nil {
			return nil, fmt.Errorf("unescape string: %w", err)
		}
		return string(unescaped), nil
	case scanner.LString, scanner.MLString:
		// Strip surrounding quotes, no unescape needed.
		return store.stripLiteralQuotes(text), nil
	case scanner.Integer:
		return strconv.ParseInt(text, 0, 64)
	case scanner.Float:
		return strconv.ParseFloat(text, 64)
	case scanner.Word:
		switch text {
		case "true":
			return true, nil
		case "false":
			return false, nil
		default:
			return text, nil
		}
	case scanner.DateTime, scanner.LocalDate, scanner.LocalTime, scanner.LocalDateTime:
		return text, nil
	default:
		return nil, fmt.Errorf("unsupported token type: %v", tok.Type)
	}
}

// stripStringQuotes removes surrounding " or """ from a basic string token text.
func (store *Store) stripStringQuotes(s string) string {
	if strings.HasPrefix(s, `"""`) && strings.HasSuffix(s, `"""`) {
		return s[3 : len(s)-3]
	}
	if strings.HasPrefix(s, `"`) && strings.HasSuffix(s, `"`) {
		return s[1 : len(s)-1]
	}
	return s
}

// stripLiteralQuotes removes surrounding ' or "' from a literal string token text.
func (store *Store) stripLiteralQuotes(s string) string {
	if strings.HasPrefix(s, `'''`) && strings.HasSuffix(s, `'''`) {
		return s[3 : len(s)-3]
	}
	if strings.HasPrefix(s, `'`) && strings.HasSuffix(s, `'`) {
		return s[1 : len(s)-1]
	}
	return s
}

// datumToGoValue converts a parser.Datum to a Go value.
func (store *Store) datumToGoValue(datum parser.Datum) (interface{}, error) {
	switch d := datum.(type) {
	case parser.Token:
		return store.tokenToGoValue(d)
	case parser.Array:
		var result []any
		for _, item := range d {
			switch ai := item.(type) {
			case parser.Value:
				val, err := store.datumToGoValue(ai.X)
				if err != nil {
					return nil, err
				}
				result = append(result, val)
				if ai.Trailer != "" {
					result = append(result, sops.Comment{Value: store.cleanCommentText(ai.Trailer)})
				}
			case parser.Comments:
				for _, line := range ai {
					result = append(result, sops.Comment{Value: store.cleanCommentText(line)})
				}
			}
		}
		if result == nil {
			result = []any{}
		}
		return result, nil
	case parser.Inline:
		var branch sops.TreeBranch
		if d.Trailer != "" {
			branch = append(branch, sops.TreeItem{
				Key:   sops.Comment{Value: store.cleanCommentText(d.Trailer)},
				Value: nil,
			})
		}
		for _, kv := range d.Items {
			name := kv.Name[len(kv.Name)-1]
			for _, line := range kv.Block {
				branch = append(branch, sops.TreeItem{
					Key:   sops.Comment{Value: store.cleanCommentText(line)},
					Value: nil,
				})
			}
			val, err := store.datumToGoValue(kv.Value.X)
			if err != nil {
				return nil, err
			}
			branch = append(branch, sops.TreeItem{Key: name, Value: val})
			if kv.Value.Trailer != "" {
				branch = append(branch, sops.TreeItem{
					Key:   sops.Comment{Value: store.cleanCommentText(kv.Value.Trailer)},
					Value: nil,
				})
			}
		}
		return branch, nil
	default:
		return nil, fmt.Errorf("unsupported datum type: %T", datum)
	}
}

// goValueToParserValue converts a Go value to a parser.Value for use in tomledit documents.
func (store *Store) goValueToParserValue(v interface{}) parser.Value {
	switch val := v.(type) {
	case string:
		return parser.MustValue(fmt.Sprintf("%q", val))
	case int64:
		return parser.MustValue(strconv.FormatInt(val, 10))
	case int:
		return parser.MustValue(strconv.FormatInt(int64(val), 10))
	case float64:
		s := strconv.FormatFloat(val, 'G', -1, 64)
		// Ensure there's a decimal point for non-scientific notation
		if !strings.ContainsRune(s, '.') && !strings.ContainsRune(s, 'E') && !strings.ContainsRune(s, 'e') {
			s = strconv.FormatFloat(val, 'f', 1, 64)
		}
		return parser.MustValue(s)
	case bool:
		if val {
			return parser.MustValue("true")
		}
		return parser.MustValue("false")
	case []any:
		return store.goSliceToParserValue(val)
	case sops.TreeBranch:
		return store.goBranchToParserValue(val)
	default:
		return parser.MustValue(fmt.Sprintf("%v", v))
	}
}

func (store *Store) goSliceToParserValue(items []any) parser.Value {
	arr := make(parser.Array, 0, len(items))
	lastValueIdx := -1
	var pendingComments parser.Comments
	for _, item := range items {
		if c, ok := item.(sops.Comment); ok {
			if lastValueIdx >= 0 {
				v := arr[lastValueIdx].(parser.Value)
				v.Trailer = "# " + c.Value
				arr[lastValueIdx] = v
				lastValueIdx = -1
			} else {
				pendingComments = append(pendingComments, "# "+c.Value)
			}
			continue
		}
		if len(pendingComments) > 0 {
			arr = append(arr, pendingComments)
			pendingComments = nil
		}
		arr = append(arr, store.goValueToParserValue(item))
		lastValueIdx = len(arr) - 1
	}
	if len(pendingComments) > 0 {
		arr = append(arr, pendingComments)
	}
	return parser.Value{X: arr}
}

func (store *Store) goBranchToParserValue(branch sops.TreeBranch) parser.Value {
	inline := parser.Inline{}
	var pendingComments parser.Comments
	isFirst := true
	for _, item := range branch {
		if c, ok := item.Key.(sops.Comment); ok {
			if isFirst && inline.Trailer == "" {
				inline.Trailer = "# " + c.Value
			} else if len(inline.Items) > 0 {
				lastKV := inline.Items[len(inline.Items)-1]
				if lastKV.Value.Trailer == "" {
					lastKV.Value.Trailer = "# " + c.Value
				} else {
					pendingComments = append(pendingComments, "# "+c.Value)
				}
			} else {
				pendingComments = append(pendingComments, "# "+c.Value)
			}
			isFirst = false
			continue
		}
		isFirst = false
		key, ok := item.Key.(string)
		if !ok {
			continue
		}
		kv := &parser.KeyValue{
			Name:  parser.Key{key},
			Value: store.goValueToParserValue(item.Value),
		}
		if len(pendingComments) > 0 {
			kv.Block = pendingComments
			pendingComments = nil
		}
		inline.Items = append(inline.Items, kv)
	}
	return parser.Value{X: inline}
}

// --- Tree navigation helpers ---

// setNestedBranch navigates the tree at root along path and sets the branch items at the leaf.
func (store *Store) setNestedBranch(root *sops.TreeBranch, path []string, items sops.TreeBranch) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		// Find or create the entry at root level
		for i, item := range *root {
			if key, ok := item.Key.(string); ok && key == path[0] {
				// Merge items into existing branch
				if existing, ok := item.Value.(sops.TreeBranch); ok {
					(*root)[i].Value = append(existing, items...)
				} else {
					(*root)[i].Value = items
				}
				return
			}
		}
		*root = append(*root, sops.TreeItem{Key: path[0], Value: items})
		return
	}
	// Navigate deeper
	for i, item := range *root {
		if key, ok := item.Key.(string); ok && key == path[0] {
			if branch, ok := item.Value.(sops.TreeBranch); ok {
				store.setNestedBranch(&branch, path[1:], items)
				(*root)[i].Value = branch
				return
			}
		}
	}
	// Create intermediate branch
	newBranch := sops.TreeBranch{}
	store.setNestedBranch(&newBranch, path[1:], items)
	*root = append(*root, sops.TreeItem{Key: path[0], Value: newBranch})
}

// appendToArrayOfTables navigates the tree along path and appends a new entry to the array of tables.
func (store *Store) appendToArrayOfTables(root *sops.TreeBranch, path []string, items sops.TreeBranch) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		for i, item := range *root {
			if key, ok := item.Key.(string); ok && key == path[0] {
				if arr, ok := item.Value.([]any); ok {
					(*root)[i].Value = append(arr, items)
				}
				return
			}
		}
		// Create new array of tables
		*root = append(*root, sops.TreeItem{Key: path[0], Value: []any{items}})
		return
	}
	// Navigate deeper
	for i, item := range *root {
		if key, ok := item.Key.(string); ok && key == path[0] {
			if branch, ok := item.Value.(sops.TreeBranch); ok {
				store.appendToArrayOfTables(&branch, path[1:], items)
				(*root)[i].Value = branch
				return
			}
		}
	}
	// Create intermediate branch
	newBranch := sops.TreeBranch{}
	store.appendToArrayOfTables(&newBranch, path[1:], items)
	*root = append(*root, sops.TreeItem{Key: path[0], Value: newBranch})
}

// addItemsToPath appends TreeItems at the given path in the tree.
func (store *Store) addItemsToPath(root *sops.TreeBranch, path []string, items []sops.TreeItem) {
	if len(path) == 0 {
		*root = append(*root, items...)
		return
	}
	if len(path) == 1 {
		for i, item := range *root {
			if key, ok := item.Key.(string); ok && key == path[0] {
				if branch, ok := item.Value.(sops.TreeBranch); ok {
					branch = append(branch, items...)
					(*root)[i].Value = branch
					return
				}
			}
		}
		return
	}
	// Navigate deeper
	for i, item := range *root {
		if key, ok := item.Key.(string); ok && key == path[0] {
			if branch, ok := item.Value.(sops.TreeBranch); ok {
				store.addItemsToPath(&branch, path[1:], items)
				(*root)[i].Value = branch
				return
			}
		}
	}
}

// addItemsToArrayOfTables adds items to the last element of an array of tables at the given path.
func (store *Store) addItemsToArrayOfTables(root *sops.TreeBranch, path []string, items []sops.TreeItem) {
	if len(path) == 0 {
		return
	}
	if len(path) == 1 {
		for i, item := range *root {
			if key, ok := item.Key.(string); ok && key == path[0] {
				if arr, ok := item.Value.([]any); ok && len(arr) > 0 {
					if lastBranch, ok := arr[len(arr)-1].(sops.TreeBranch); ok {
						arr[len(arr)-1] = append(lastBranch, items...)
						(*root)[i].Value = arr
					}
				}
				return
			}
		}
		return
	}
	for i, item := range *root {
		if key, ok := item.Key.(string); ok && key == path[0] {
			if branch, ok := item.Value.(sops.TreeBranch); ok {
				store.addItemsToArrayOfTables(&branch, path[1:], items)
				(*root)[i].Value = branch
				return
			}
		}
	}
}

// --- Type checks ---

func (store *Store) isArrayOfTables(v []any) bool {
	hasTable := false
	for _, item := range v {
		if _, ok := item.(sops.Comment); ok {
			continue
		}
		if _, ok := item.(sops.TreeBranch); !ok {
			return false
		}
		hasTable = true
	}
	return hasTable
}

func (store *Store) isComplexGoValue(v interface{}) bool {
	switch val := v.(type) {
	case sops.TreeBranch:
		return true
	case []any:
		return !store.isArrayOfTables(val)
	}
	return false
}

// --- Utility functions ---

func (store *Store) marshalMetadata(metadata stores.Metadata) ([]byte, error) {
	wrapper := struct {
		Sops stores.Metadata `toml:"sops"`
	}{Sops: metadata}
	var buf bytes.Buffer
	enc := toml.NewEncoder(&buf)
	enc.SetIndentTables(true)
	if err := enc.Encode(wrapper); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// --- LoadPlainFile ---

func (store *Store) LoadPlainFile(in []byte) (sops.TreeBranches, error) {
	if len(bytes.TrimSpace(in)) == 0 {
		return sops.TreeBranches{sops.TreeBranch{}}, nil
	}

	doc, err := tomledit.Parse(bytes.NewReader(in))
	if err != nil {
		return nil, fmt.Errorf("could not parse TOML: %w", err)
	}

	var root sops.TreeBranch

	// Track previous section location for heading block comment placement
	type prevSection struct {
		path    []string
		isArray bool
	}
	var prev *prevSection

	loadSection := func(items []parser.Item, heading *parser.Heading, isGlobal bool, path []string, isArray bool) {
		// Heading block comments → append to end of previous section
		if heading != nil && len(heading.Block) > 0 && prev != nil {
			var headingComments []sops.TreeItem
			for _, line := range heading.Block {
				headingComments = append(headingComments, sops.TreeItem{
					Key:   sops.Comment{Value: store.cleanCommentText(line)},
					Value: nil,
				})
			}
			if prev.isArray {
				store.addItemsToArrayOfTables(&root, prev.path, headingComments)
			} else if prev.path != nil {
				store.addItemsToPath(&root, prev.path, headingComments)
			} else {
				root = append(root, headingComments...)
			}
		}

		// Process items into tree items and parent-scope comments
		var sectionItems []sops.TreeItem
		var parentComments []sops.TreeItem

		for _, item := range items {
			switch it := item.(type) {
			case parser.Comments:
				for _, line := range it {
					sectionItems = append(sectionItems, sops.TreeItem{
						Key:   sops.Comment{Value: store.cleanCommentText(line)},
						Value: nil,
					})
				}
			case *parser.KeyValue:
				name := it.Name[len(it.Name)-1]
				val, err := store.datumToGoValue(it.Value.X)
				if err != nil {
					continue
				}

				// KV block comments
				for _, line := range it.Block {
					commentItem := sops.TreeItem{
						Key:   sops.Comment{Value: store.cleanCommentText(line)},
						Value: nil,
					}
					if isGlobal {
						sectionItems = append(sectionItems, commentItem)
					} else {
						parentComments = append(parentComments, commentItem)
					}
				}

				// Trailing comments on complex values → before KV
				if store.isComplexGoValue(val) && it.Value.Trailer != "" {
					sectionItems = append(sectionItems, sops.TreeItem{
						Key:   sops.Comment{Value: store.cleanCommentText(it.Value.Trailer)},
						Value: nil,
					})
				}

				sectionItems = append(sectionItems, sops.TreeItem{Key: name, Value: val})

				// Trailing comments on scalar values → after KV
				if !store.isComplexGoValue(val) && it.Value.Trailer != "" {
					sectionItems = append(sectionItems, sops.TreeItem{
						Key:   sops.Comment{Value: store.cleanCommentText(it.Value.Trailer)},
						Value: nil,
					})
				}
			}
		}

		// Insert parent comments at parent scope
		if len(parentComments) > 0 {
			if len(path) > 1 {
				store.addItemsToPath(&root, path[:len(path)-1], parentComments)
			} else if len(path) == 1 {
				root = append(root, parentComments...)
			}
		}

		// Insert section items
		if path == nil {
			root = append(root, sectionItems...)
		} else if isArray {
			store.appendToArrayOfTables(&root, path, sops.TreeBranch(sectionItems))
		} else {
			store.setNestedBranch(&root, path, sops.TreeBranch(sectionItems))
		}

		prev = &prevSection{path: path, isArray: isArray}
	}

	// Process global section
	if doc.Global != nil {
		loadSection(doc.Global.Items, nil, true, nil, false)
	}

	// Process named sections
	for _, sec := range doc.Sections {
		isArray := sec.Heading != nil && sec.Heading.IsArray
		path := make([]string, len(sec.TableName()))
		copy(path, sec.TableName())
		loadSection(sec.Items, sec.Heading, false, path, isArray)
	}

	return sops.TreeBranches{root}, nil
}

// --- EmitPlainFile ---

func (store *Store) EmitPlainFile(in sops.TreeBranches) ([]byte, error) {
	if len(in) > 1 {
		return nil, ErrTOMLUniqueDocument
	}
	if len(in) == 0 || len(in[0]) == 0 {
		return []byte{}, nil
	}

	branch := in[0]
	doc := &tomledit.Document{
		Global: &tomledit.Section{},
	}

	headingCommentBuf := &[]sops.TreeItem{}
	store.walkBranch(branch, nil, doc, headingCommentBuf, false)

	// Flush remaining heading comments as standalone comments on the last section.
	if len(*headingCommentBuf) > 0 {
		lastSection := doc.Global
		if len(doc.Sections) > 0 {
			lastSection = doc.Sections[len(doc.Sections)-1]
		}
		for _, item := range *headingCommentBuf {
			if c, ok := item.Key.(sops.Comment); ok {
				lastSection.Items = append(lastSection.Items, parser.Comments{"# " + c.Value})
			}
		}
	}

	var buf bytes.Buffer
	if err := tomledit.Format(&buf, doc); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (store *Store) walkBranch(branch sops.TreeBranch, prefix []string, doc *tomledit.Document, headingCommentBuf *[]sops.TreeItem, isArray bool) {
	var section *tomledit.Section
	if prefix == nil {
		section = doc.Global
	} else {
		heading := &parser.Heading{
			Name:    parser.Key(prefix),
			IsArray: isArray,
		}
		// Apply heading comment buffer as heading block
		if len(*headingCommentBuf) > 0 {
			for _, item := range *headingCommentBuf {
				if c, ok := item.Key.(sops.Comment); ok {
					heading.Block = append(heading.Block, "# "+c.Value)
				}
			}
			*headingCommentBuf = nil
		}
		section = &tomledit.Section{Heading: heading}
		doc.Sections = append(doc.Sections, section)
	}

	var pendingComments []string
	var lastScalarKV *parser.KeyValue

	for idx, item := range branch {
		if c, ok := item.Key.(sops.Comment); ok {
			// Check if there are more data items after this comment
			hasMoreData := false
			for j := idx + 1; j < len(branch); j++ {
				if _, isComment := branch[j].Key.(sops.Comment); !isComment {
					hasMoreData = true
					break
				}
			}
			if !hasMoreData {
				// Buffer to headingCommentBuf for next section
				*headingCommentBuf = append(*headingCommentBuf, sops.TreeItem{Key: sops.Comment{Value: c.Value}, Value: nil})
			} else if lastScalarKV != nil {
				// Set as trailing comment on last scalar KV
				lastScalarKV.Value.Trailer = "# " + c.Value
				lastScalarKV = nil
			} else {
				// Buffer to pending comments
				pendingComments = append(pendingComments, "# "+c.Value)
			}
			continue
		}

		key, ok := item.Key.(string)
		if !ok {
			continue
		}

		val := item.Value
		if subBranch, ok := val.(sops.TreeBranch); ok {
			// Sub-table
			kvBlockForChild := pendingComments
			pendingComments = nil
			lastScalarKV = nil

			childPrefix := append(append([]string{}, prefix...), key)
			store.walkBranch(subBranch, childPrefix, doc, headingCommentBuf, false)

			// Apply kvBlockForChild to the first KV in the child's section
			if len(kvBlockForChild) > 0 {
				store.applyBlockToSection(doc, childPrefix, kvBlockForChild)
			}
			continue
		}

		if arr, ok := val.([]any); ok && store.isArrayOfTables(arr) {
			// Array of tables
			kvBlockForChild := pendingComments
			pendingComments = nil
			lastScalarKV = nil

			entryIdx := 0
			for _, elem := range arr {
				if c, ok := elem.(sops.Comment); ok {
					*headingCommentBuf = append(*headingCommentBuf, sops.TreeItem{Key: sops.Comment{Value: c.Value}, Value: nil})
					continue
				}
				if b, ok := elem.(sops.TreeBranch); ok {
					childPrefix := append(append([]string{}, prefix...), key)
					store.walkBranch(b, childPrefix, doc, headingCommentBuf, true)
					// Apply kvBlockForChild only to first entry
					if entryIdx == 0 && len(kvBlockForChild) > 0 {
						store.applyBlockToSection(doc, childPrefix, kvBlockForChild)
					}
					entryIdx++
				}
			}
			continue
		}

		// Scalar or complex (array/inline table) value
		kv := &parser.KeyValue{
			Name:  parser.Key{key},
			Value: store.goValueToParserValue(val),
		}

		if store.isComplexGoValue(val) {
			// Complex value: pending comments → last as trailer, rest as block
			if len(pendingComments) > 0 {
				kv.Value.Trailer = pendingComments[len(pendingComments)-1]
				if len(pendingComments) > 1 {
					kv.Block = parser.Comments(pendingComments[:len(pendingComments)-1])
				}
				pendingComments = nil
			}
			lastScalarKV = nil
		} else {
			// Scalar value: pending comments → block
			if len(pendingComments) > 0 {
				kv.Block = parser.Comments(pendingComments)
				pendingComments = nil
			}
			lastScalarKV = kv
		}

		section.Items = append(section.Items, kv)
	}

	// Remaining pending comments → headingCommentBuf
	for _, c := range pendingComments {
		*headingCommentBuf = append(*headingCommentBuf, sops.TreeItem{
			Key:   sops.Comment{Value: store.cleanCommentText(c)},
			Value: nil,
		})
	}
}

// applyBlockToSection finds the first section matching prefix and applies block comments
// to its first KV or heading.
func (store *Store) applyBlockToSection(doc *tomledit.Document, prefix []string, block []string) {
	target := parser.Key(prefix)
	for _, sec := range doc.Sections {
		if sec.Heading != nil && sec.Heading.Name.Equals(target) {
			// Find the first KV in this section
			for _, item := range sec.Items {
				if kv, ok := item.(*parser.KeyValue); ok {
					kv.Block = append(parser.Comments(block), kv.Block...)
					return
				}
			}
			// If no KV, put it on the heading
			sec.Heading.Block = append(parser.Comments(block), sec.Heading.Block...)
			return
		}
	}
}

// --- Simple methods ---

func (store *Store) HasSopsTopLevelKey(branch sops.TreeBranch) bool {
	return stores.HasSopsTopLevelKey(branch)
}

func (store *Store) EmitValue(v interface{}) ([]byte, error) {
	switch val := v.(type) {
	case sops.TreeBranch:
		return store.EmitPlainFile(sops.TreeBranches{val})
	case string:
		return []byte(fmt.Sprintf("%q", val)), nil
	case bool:
		return []byte(strconv.FormatBool(val)), nil
	case int:
		return []byte(strconv.FormatInt(int64(val), 10)), nil
	case int64:
		return []byte(strconv.FormatInt(val, 10)), nil
	case float64:
		s := strconv.FormatFloat(val, 'G', -1, 64)
		if !strings.ContainsRune(s, '.') && !strings.ContainsRune(s, 'E') && !strings.ContainsRune(s, 'e') {
			s = strconv.FormatFloat(val, 'f', 1, 64)
		}
		return []byte(s), nil
	default:
		return []byte(fmt.Sprintf("%v", v)), nil
	}
}

func (store *Store) EmitExample() []byte {
	bytes, err := store.EmitPlainFile(stores.ExampleComplexTree.Branches)
	if err != nil {
		panic(err)
	}
	return bytes
}

// --- LoadEncryptedFile ---

func (store *Store) LoadEncryptedFile(in []byte) (sops.Tree, error) {
	// Use pelletier/go-toml to unmarshal metadata
	var sopsFile stores.SopsFile
	if err := toml.Unmarshal(in, &sopsFile); err != nil {
		return sops.Tree{}, fmt.Errorf("could not unmarshal TOML: %w", err)
	}
	if sopsFile.Metadata == nil {
		return sops.Tree{}, sops.MetadataNotFound
	}

	metadata, err := sopsFile.Metadata.ToInternal()
	if err != nil {
		return sops.Tree{}, err
	}

	// Load data using our AST parser
	branches, err := store.LoadPlainFile(in)
	if err != nil {
		return sops.Tree{}, fmt.Errorf("could not load TOML data: %w", err)
	}

	// Remove "sops" key from branches
	for bi, branch := range branches {
		for i, item := range branch {
			if key, ok := item.Key.(string); ok && key == stores.SopsMetadataKey {
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

// --- EmitEncryptedFile ---

func (store *Store) EmitEncryptedFile(in sops.Tree) ([]byte, error) {
	if len(in.Branches) > 1 {
		return nil, ErrTOMLUniqueDocument
	}

	// Build data portion
	dataBytes, err := store.EmitPlainFile(in.Branches)
	if err != nil {
		return nil, fmt.Errorf("could not emit TOML data: %w", err)
	}

	// Build metadata portion
	meta := stores.MetadataFromInternal(in.Metadata)
	metaBytes, err := store.marshalMetadata(meta)
	if err != nil {
		return nil, fmt.Errorf("could not marshal metadata: %w", err)
	}

	// Concatenate
	var buf bytes.Buffer
	buf.Write(dataBytes)
	if len(dataBytes) > 0 && !bytes.HasSuffix(dataBytes, []byte("\n")) {
		buf.WriteByte('\n')
	}
	buf.WriteByte('\n')
	buf.Write(metaBytes)

	return buf.Bytes(), nil
}
