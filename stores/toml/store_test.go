package toml

import (
	"testing"
	"time"

	"github.com/getsops/sops/v3"
	"github.com/getsops/sops/v3/age"
	"github.com/getsops/sops/v3/config"
	"github.com/getsops/sops/v3/stores"
	toml "github.com/pelletier/go-toml/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const testInput = `# Booleans
bool_true = true
bool_false = false
# Integers: decimal, signed, hex, octal, binary, underscores
int_dec = 42
int_zero = 0
int_pos = +99
int_neg = -17
int_hex = 0xDEADBEEF
int_hex_lower = 0xdead_beef
int_oct = 0o755
int_bin = 0b11010110
int_underscore = 1_000_000
# Floats: regular, signed, exponent, combined, special values
flt = 3.14
flt_pos = +1.0
flt_neg = -0.01
flt_exp = 5e+22
flt_neg_exp = 1e-06
flt_combined = 6.626e-34
flt_underscore = 9_224_617.445_991_228_313
flt_inf = inf
flt_pos_inf = +inf
flt_neg_inf = -inf
flt_nan = nan
# Strings: literal
str_simple = 'hello world'
str_empty = ''
str_unicode = 'café'
str_tab = 'has	tab'
str_backslash = 'back\slash'
str_dquote = 'has"dquote'
str_special_chars = 'path/to/file.txt'
# Strings: basic (required when value contains ' \r \n or control chars)
str_squote = "it's"
str_newline = "line1\nline2"
str_cr = "has\rreturn"
str_both_escapes = "has\\both\n"
str_all_escapes = "bs\b ff\f cr\r lf\n tab\t"
str_control = "esc\u001B"
# Local dates
ld = 2024-01-15
# Local times
lt = 14:30:00
lt_frac = 14:30:00.123456
# Local datetimes
ldt = 2024-01-15T14:30:00
ldt_frac = 2024-01-15T14:30:00.999
# Offset datetimes: Z, positive offset, negative offset, fractional
odt_z = 2024-01-15T14:30:00Z
odt_pos = 2024-01-15T14:30:00+09:00
odt_neg = 2024-01-15T14:30:00-05:00
odt_frac = 2024-01-15T14:30:00.123+09:00
odt_space = 2024-01-15 14:30:00Z
# Keys: bare, literal-quoted, basic-quoted, empty, dotted, mixed
bare_key = 1
'key with spaces' = 2
"key'quote" = 3
'' = 4
a.b.c = 5
a.'b c'.d = 6
# Tables
[simple]
[dotted.table.key]
['table with spaces']
# Array tables
[[array-table]]
[[dotted.array.table]]
# Arrays: empty, single, inline, nested, mixed types
empty_arr = []
single_arr = [42]
int_arr = [1, 2, 3]
str_arr = ['web', 'dev', 'go']
nested_arr = [[1, 2], [3, 4]]
mixed_arr = [1, 'two', 3.0, true, 2024-01-15]
# Inline tables: single entry, multiple entries
single_tbl = {val = 1}
point_tbl = {x = 1, y = 2}
name_tbl = {first = 'Tom', last = 'Doe'}
# Multiline array with comments
multiline_arr = [ # array header
  # before first
  1, # first
  # between
  2,
  3, # last
  # after last
]
# Multiline inline table with comments
multiline_tbl = { # table header
  host = 'localhost', # the host
  port = 8080,
}
`

const testOutput = `# Booleans
bool_true = true
bool_false = false
# Integers: decimal, signed, hex, octal, binary, underscores
int_dec = 42
int_zero = 0
int_pos = 99
int_neg = -17
int_hex = 3735928559
int_hex_lower = 3735928559
int_oct = 493
int_bin = 214
int_underscore = 1000000
# Floats: regular, signed, exponent, combined, special values
flt = 3.14
flt_pos = 1.0
flt_neg = -0.01
flt_exp = 5e+22
flt_neg_exp = 1e-06
flt_combined = 6.626e-34
flt_underscore = 9.224617445991227e+06
flt_inf = inf
flt_pos_inf = inf
flt_neg_inf = -inf
flt_nan = nan
# Strings: literal
str_simple = 'hello world'
str_empty = ''
str_unicode = 'café'
str_tab = 'has	tab'
str_backslash = 'back\slash'
str_dquote = 'has"dquote'
str_special_chars = 'path/to/file.txt'
# Strings: basic (required when value contains ' \r \n or control chars)
str_squote = "it's"
str_newline = "line1\nline2"
str_cr = "has\rreturn"
str_both_escapes = "has\\both\n"
str_all_escapes = "bs\b ff\f cr\r lf\n tab\t"
str_control = "esc\u001B"
# Local dates
ld = 2024-01-15
# Local times
lt = 14:30:00
lt_frac = 14:30:00.123456
# Local datetimes
ldt = 2024-01-15T14:30:00
ldt_frac = 2024-01-15T14:30:00.999
# Offset datetimes: Z, positive offset, negative offset, fractional
odt_z = 2024-01-15T14:30:00Z
odt_pos = 2024-01-15T14:30:00+09:00
odt_neg = 2024-01-15T14:30:00-05:00
odt_frac = 2024-01-15T14:30:00.123+09:00
odt_space = 2024-01-15T14:30:00Z
# Keys: bare, literal-quoted, basic-quoted, empty, dotted, mixed
bare_key = 1
'key with spaces' = 2
"key'quote" = 3
'' = 4

[a]

[a.b]
c = 5

[a.'b c']
d = 6

# Tables
[simple]

[dotted]

[dotted.table]

[dotted.table.key]

['table with spaces']

# Array tables
[[array-table]]

[dotted.array]

[[dotted.array.table]]
# Arrays: empty, single, inline, nested, mixed types
empty_arr = []
single_arr = [42]
int_arr = [1, 2, 3]
str_arr = ['web', 'dev', 'go']
nested_arr = [[1, 2], [3, 4]]
mixed_arr = [1, 'two', 3.0, true, 2024-01-15]
# Multiline array with comments
multiline_arr = [ # array header
  # before first
  1, # first
  # between
  2,
  3, # last
  # after last
]

# Inline tables: single entry, multiple entries
[dotted.array.table.single_tbl]
val = 1

[dotted.array.table.point_tbl]
x = 1
y = 2

[dotted.array.table.name_tbl]
first = 'Tom'
last = 'Doe'

# Multiline inline table with comments
[dotted.array.table.multiline_tbl] # table header
host = 'localhost' # the host
port = 8080
`

func TestLoadPlainFileRoundTrip(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branches, err := store.LoadPlainFile([]byte(testInput))
	require.NoError(t, err)

	output1, err := store.EmitPlainFile(branches)
	require.NoError(t, err)

	t.Log(string(output1))
	assert.Equal(t, testOutput, string(output1))

	var inputAsMap, outputAsMap map[any]any
	err = toml.Unmarshal([]byte(testInput), &inputAsMap)
	require.NoError(t, err)
	err = toml.Unmarshal([]byte(output1), &outputAsMap)
	require.NoError(t, err)
	// Delete NaN entries: reflect.DeepEqual (used by assert.Equal) considers NaN != NaN.
	delete(inputAsMap, "flt_nan")
	delete(outputAsMap, "flt_nan")
	assert.Equal(t, inputAsMap, outputAsMap)

	branches2, err := store.LoadPlainFile(output1)
	require.NoError(t, err)

	output2, err := store.EmitPlainFile(branches2)
	require.NoError(t, err)

	// The first emit normalizes: inline tables → sections, dotted keys → sections,
	// hex/oct/bin integers → decimal. After normalization, subsequent load→emit
	// cycles must be fully stable.
	assert.Equal(t, string(output1), string(output2)) // stable text roundtrip

	// Verify tree stability by checking that a third emit produces the same output.
	// We use text comparison rather than tree equality because NaN != NaN in Go.
	branches3, err := store.LoadPlainFile(output2)
	require.NoError(t, err)

	output3, err := store.EmitPlainFile(branches3)
	require.NoError(t, err)

	assert.Equal(t, string(output2), string(output3)) // still stable
}

// ---------------------------------------------------------------------------
// Name
// ---------------------------------------------------------------------------

func TestName(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	assert.Equal(t, "toml", store.Name())
}

// ---------------------------------------------------------------------------
// LoadPlainFile
// ---------------------------------------------------------------------------

func TestLoadPlainFileEmpty(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branches, err := store.LoadPlainFile([]byte(""))
	require.NoError(t, err)
	assert.Equal(t, sops.TreeBranches{nil}, branches)
}

func TestLoadPlainFileSimpleKV(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branches, err := store.LoadPlainFile([]byte("key = 'value'\n"))
	require.NoError(t, err)
	require.Len(t, branches, 1)
	assert.Equal(t, sops.TreeBranch{
		sops.TreeItem{Key: "key", Value: "value"},
	}, branches[0])
}

func TestLoadPlainFileInvalidTOML(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	_, err := store.LoadPlainFile([]byte("[invalid"))
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "could not unmarshal TOML data")
}

func TestLoadPlainFileAllTypes(t *testing.T) {
	input := `str = 'hello'
int = 42
flt = 3.14
b = true
dt = 2024-01-15T14:30:00Z
ld = 2024-01-15
lt = 14:30:00
ldt = 2024-01-15T14:30:00
`
	store := NewStore(&config.TOMLStoreConfig{})
	branches, err := store.LoadPlainFile([]byte(input))
	require.NoError(t, err)
	require.Len(t, branches, 1)
	branch := branches[0]

	assert.Equal(t, "hello", branch[0].Value)
	assert.Equal(t, int64(42), branch[1].Value)
	assert.Equal(t, 3.14, branch[2].Value)
	assert.Equal(t, true, branch[3].Value)
	assert.IsType(t, time.Time{}, branch[4].Value)
	assert.IsType(t, toml.LocalDate{}, branch[5].Value)
	assert.IsType(t, toml.LocalTime{}, branch[6].Value)
	assert.IsType(t, toml.LocalDateTime{}, branch[7].Value)
}

func TestLoadPlainFileWithSections(t *testing.T) {
	input := `[server]
host = 'localhost'
port = 8080
`
	store := NewStore(&config.TOMLStoreConfig{})
	branches, err := store.LoadPlainFile([]byte(input))
	require.NoError(t, err)
	require.Len(t, branches, 1)
	require.Len(t, branches[0], 1)

	item := branches[0][0]
	assert.Equal(t, "server", item.Key)
	sub := item.Value.(sops.TreeBranch)
	assert.Equal(t, "host", sub[0].Key)
	assert.Equal(t, "localhost", sub[0].Value)
	assert.Equal(t, "port", sub[1].Key)
	assert.Equal(t, int64(8080), sub[1].Value)
}

func TestLoadPlainFileWithArrayTables(t *testing.T) {
	input := `[[servers]]
name = 'alpha'

[[servers]]
name = 'beta'
`
	store := NewStore(&config.TOMLStoreConfig{})
	branches, err := store.LoadPlainFile([]byte(input))
	require.NoError(t, err)
	require.Len(t, branches, 1)
	require.Len(t, branches[0], 1)

	item := branches[0][0]
	assert.Equal(t, "servers", item.Key)
	arr := item.Value.([]any)
	require.Len(t, arr, 2)
	assert.Equal(t, "alpha", arr[0].(sops.TreeBranch)[0].Value)
	assert.Equal(t, "beta", arr[1].(sops.TreeBranch)[0].Value)
}

func TestLoadPlainFileWithComments(t *testing.T) {
	input := `# top comment
key = 'value'
`
	store := NewStore(&config.TOMLStoreConfig{})
	branches, err := store.LoadPlainFile([]byte(input))
	require.NoError(t, err)
	require.Len(t, branches, 1)
	require.Len(t, branches[0], 2)

	c, ok := branches[0][0].Key.(sops.Comment)
	assert.True(t, ok)
	assert.Equal(t, "top comment", c.Value)
	assert.Equal(t, "key", branches[0][1].Key)
}

func TestLoadPlainFileWithTrailingComment(t *testing.T) {
	input := `key = 'value' # trailing
`
	store := NewStore(&config.TOMLStoreConfig{})
	branches, err := store.LoadPlainFile([]byte(input))
	require.NoError(t, err)
	require.Len(t, branches, 1)
	require.Len(t, branches[0], 2)

	assert.Equal(t, "key", branches[0][0].Key)
	c, ok := branches[0][1].Key.(sops.Comment)
	assert.True(t, ok)
	assert.Equal(t, "trailing", c.Value)
	assert.True(t, c.Trailer)
}

// ---------------------------------------------------------------------------
// EmitPlainFile
// ---------------------------------------------------------------------------

func TestEmitPlainFileEmpty(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	out, err := store.EmitPlainFile(sops.TreeBranches{})
	require.NoError(t, err)
	assert.Equal(t, []byte{}, out)
}

func TestEmitPlainFileSimpleKV(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branches := sops.TreeBranches{
		sops.TreeBranch{
			sops.TreeItem{Key: "name", Value: "test"},
			sops.TreeItem{Key: "count", Value: int64(5)},
		},
	}
	out, err := store.EmitPlainFile(branches)
	require.NoError(t, err)
	assert.Equal(t, "name = 'test'\ncount = 5\n", string(out))
}

func TestEmitPlainFileMultipleDocumentsError(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branches := sops.TreeBranches{
		sops.TreeBranch{sops.TreeItem{Key: "a", Value: "1"}},
		sops.TreeBranch{sops.TreeItem{Key: "b", Value: "2"}},
	}
	_, err := store.EmitPlainFile(branches)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "can only contain one document")
}

func TestEmitPlainFileWithSection(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branches := sops.TreeBranches{
		sops.TreeBranch{
			sops.TreeItem{
				Key: "db",
				Value: sops.TreeBranch{
					sops.TreeItem{Key: "host", Value: "localhost"},
				},
			},
		},
	}
	out, err := store.EmitPlainFile(branches)
	require.NoError(t, err)
	assert.Equal(t, "\n[db]\nhost = 'localhost'\n", string(out))
}

func TestEmitPlainFileWithArrayTable(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branches := sops.TreeBranches{
		sops.TreeBranch{
			sops.TreeItem{
				Key: "servers",
				Value: []any{
					sops.TreeBranch{
						sops.TreeItem{Key: "name", Value: "alpha"},
					},
					sops.TreeBranch{
						sops.TreeItem{Key: "name", Value: "beta"},
					},
				},
			},
		},
	}
	out, err := store.EmitPlainFile(branches)
	require.NoError(t, err)
	expected := "\n[[servers]]\nname = 'alpha'\n\n[[servers]]\nname = 'beta'\n"
	assert.Equal(t, expected, string(out))
}

func TestEmitPlainFileWithComments(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branches := sops.TreeBranches{
		sops.TreeBranch{
			sops.TreeItem{Key: sops.Comment{Value: "a comment"}, Value: nil},
			sops.TreeItem{Key: "key", Value: "value"},
			sops.TreeItem{Key: sops.Comment{Value: "trailing", Trailer: true}, Value: nil},
		},
	}
	out, err := store.EmitPlainFile(branches)
	require.NoError(t, err)
	assert.Equal(t, "# a comment\nkey = 'value' # trailing\n", string(out))
}

func TestEmitPlainFileAllValueTypes(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branches := sops.TreeBranches{
		sops.TreeBranch{
			sops.TreeItem{Key: "s", Value: "hello"},
			sops.TreeItem{Key: "i", Value: int64(42)},
			sops.TreeItem{Key: "f", Value: 3.14},
			sops.TreeItem{Key: "b", Value: true},
			sops.TreeItem{Key: "b2", Value: false},
			sops.TreeItem{Key: "arr", Value: []any{int64(1), int64(2)}},
		},
	}
	out, err := store.EmitPlainFile(branches)
	require.NoError(t, err)
	expected := "s = 'hello'\ni = 42\nf = 3.14\nb = true\nb2 = false\narr = [1, 2]\n"
	assert.Equal(t, expected, string(out))
}

// ---------------------------------------------------------------------------
// LoadEncryptedFile
// ---------------------------------------------------------------------------

func TestLoadEncryptedFileNoMetadata(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	_, err := store.LoadEncryptedFile([]byte("hello = 'world'\n"))
	assert.Equal(t, sops.MetadataNotFound, err)
}

func TestLoadEncryptedFileInvalidTOML(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	_, err := store.LoadEncryptedFile([]byte("[invalid"))
	assert.Error(t, err)
}

func TestLoadEncryptedFileWithMetadata(t *testing.T) {
	input := `mykey = 'myvalue'

[sops]
version = '3.9.0'
lastmodified = '2024-01-15T10:00:00Z'
mac = 'AAAAAA'

[[sops.age]]
recipient = 'age1xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx'
enc = 'encrypted-data-key'
`
	store := NewStore(&config.TOMLStoreConfig{})
	tree, err := store.LoadEncryptedFile([]byte(input))
	require.NoError(t, err)
	assert.Equal(t, "3.9.0", tree.Metadata.Version)
	assert.Equal(t, "AAAAAA", tree.Metadata.MessageAuthenticationCode)

	// The sops key should be stripped from branches
	for _, branch := range tree.Branches {
		for _, item := range branch {
			assert.NotEqual(t, stores.SopsMetadataKey, item.Key)
		}
	}
	// Data should be preserved
	require.Len(t, tree.Branches, 1)
	assert.Equal(t, "mykey", tree.Branches[0][0].Key)
	assert.Equal(t, "myvalue", tree.Branches[0][0].Value)
}

// ---------------------------------------------------------------------------
// EmitEncryptedFile
// ---------------------------------------------------------------------------

func TestEmitEncryptedFile(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	tree := sops.Tree{
		Branches: sops.TreeBranches{
			sops.TreeBranch{
				sops.TreeItem{Key: "secret", Value: "ENC[AES256_GCM,data:abc]"},
			},
		},
		Metadata: sops.Metadata{
			Version:                      "3.9.0",
			LastModified:                 time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			MessageAuthenticationCode:    "test-mac",
		},
	}
	out, err := store.EmitEncryptedFile(tree)
	require.NoError(t, err)

	// Verify it contains the data
	assert.Contains(t, string(out), "secret = 'ENC[AES256_GCM,data:abc]'")
	// Verify it contains sops metadata section
	assert.Contains(t, string(out), "[sops]")
	assert.Contains(t, string(out), "version = '3.9.0'")
	assert.Contains(t, string(out), "mac = 'test-mac'")
}

func TestEmitEncryptedFileRoundTrip(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})

	ageKey, err := age.MasterKeyFromRecipient("age1lzd99uklcjnc0e7d860axevet2cz99ce9pq6tzuzd05l5nr28ams36nvun")
	require.NoError(t, err)
	ageKey.EncryptedKey = "encrypted-data-key"

	tree := sops.Tree{
		Branches: sops.TreeBranches{
			sops.TreeBranch{
				sops.TreeItem{Key: "password", Value: "encrypted_value"},
			},
		},
		Metadata: sops.Metadata{
			Version:                   "3.9.0",
			LastModified:              time.Date(2024, 1, 15, 10, 0, 0, 0, time.UTC),
			MessageAuthenticationCode: "mac-value",
			KeyGroups: []sops.KeyGroup{
				{ageKey},
			},
		},
	}

	out, err := store.EmitEncryptedFile(tree)
	require.NoError(t, err)

	loaded, err := store.LoadEncryptedFile(out)
	require.NoError(t, err)

	assert.Equal(t, "3.9.0", loaded.Metadata.Version)
	assert.Equal(t, "mac-value", loaded.Metadata.MessageAuthenticationCode)
	require.Len(t, loaded.Branches, 1)
	assert.Equal(t, "password", loaded.Branches[0][0].Key)
	assert.Equal(t, "encrypted_value", loaded.Branches[0][0].Value)
}

// ---------------------------------------------------------------------------
// EmitValue
// ---------------------------------------------------------------------------

func TestEmitValueString(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	out, err := store.EmitValue("hello")
	require.NoError(t, err)
	assert.Equal(t, []byte("'hello'"), out)
}

func TestEmitValueInt(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	out, err := store.EmitValue(42)
	require.NoError(t, err)
	assert.Equal(t, []byte("42"), out)
}

func TestEmitValueBool(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	out, err := store.EmitValue(true)
	require.NoError(t, err)
	assert.Equal(t, []byte("true"), out)
}

func TestEmitValueTreeBranches(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branches := sops.TreeBranches{
		sops.TreeBranch{
			sops.TreeItem{Key: "key", Value: "value"},
		},
	}
	out, err := store.EmitValue(branches)
	require.NoError(t, err)
	assert.Equal(t, "key = 'value'\n", string(out))
}

// ---------------------------------------------------------------------------
// EmitExample
// ---------------------------------------------------------------------------

func TestEmitExample(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	out := store.EmitExample()
	assert.NotEmpty(t, out)

	// Should be valid TOML
	var parsed map[string]any
	err := toml.Unmarshal(out, &parsed)
	require.NoError(t, err)
	assert.NotEmpty(t, parsed)
}

// ---------------------------------------------------------------------------
// HasSopsTopLevelKey
// ---------------------------------------------------------------------------

func TestHasSopsTopLevelKeyTrue(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branch := sops.TreeBranch{
		sops.TreeItem{Key: "data", Value: "value"},
		sops.TreeItem{Key: "sops", Value: sops.TreeBranch{}},
	}
	assert.True(t, store.HasSopsTopLevelKey(branch))
}

func TestHasSopsTopLevelKeyFalse(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branch := sops.TreeBranch{
		sops.TreeItem{Key: "data", Value: "value"},
	}
	assert.False(t, store.HasSopsTopLevelKey(branch))
}

func TestHasSopsTopLevelKeyEmpty(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	assert.False(t, store.HasSopsTopLevelKey(sops.TreeBranch{}))
}

func TestHasSopsTopLevelKeyPartialMatch(t *testing.T) {
	store := NewStore(&config.TOMLStoreConfig{})
	branch := sops.TreeBranch{
		sops.TreeItem{Key: "sops_extra", Value: "value"},
	}
	assert.False(t, store.HasSopsTopLevelKey(branch))
}
