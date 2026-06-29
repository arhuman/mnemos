package storage

// Test-only handles for unexported accessors. The embedding getter/deleter and
// decodeVector have no production callers — they exist for black-box tests and
// diagnostics — so they stay out of the package's importable API. Exposing them
// here, in a _test.go file, keeps the external storage_test package's coverage
// intact without widening the production surface. (CountEmbeddings remains a
// real export: search_test depends on it across packages.)
var (
	GetEmbedding    = getEmbedding
	DeleteEmbedding = deleteEmbedding
	DecodeVector    = decodeVector
)
