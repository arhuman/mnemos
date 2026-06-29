package parse

import (
	"context"
	"go/ast"
	"go/parser"
	"go/token"
	"strings"

	"github.com/arhuman/mnemos/internal/model"
)

// GoParser parses Go source with the standard go/parser, emitting one section
// per top-level declaration (func, method, type) including its leading doc
// comment, with exact line ranges from the token.FileSet. When parsing fails
// (e.g. a non-compiling snippet) it falls back to a single whole-file section
// that the code chunker windows by lines.
type GoParser struct{}

// Parse implements parse.Parser.
func (GoParser) Parse(_ context.Context, src model.Source) (model.ParsedDoc, error) {
	doc := model.ParsedDoc{
		Kind:  model.KindGoCode,
		Title: src.URI,
	}

	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, src.URI, src.Content, parser.ParseComments)
	if err != nil {
		doc.Sections = []model.Section{wholeFileSection(src.Content)}

		return doc, nil //nolint:nilerr // a parse failure deliberately falls back to a whole-file section; not a caller-facing error
	}

	if file.Name != nil {
		doc.Title = file.Name.Name
	}

	for _, decl := range file.Decls {
		start, end := declLineRange(fset, decl)
		name := declName(decl)
		doc.Sections = append(doc.Sections, model.Section{
			HeadingPath: name,
			StartLine:   start,
			EndLine:     end,
			Content:     sliceLines(src.Content, start, end),
		})
	}
	if len(doc.Sections) == 0 {
		doc.Sections = []model.Section{wholeFileSection(src.Content)}
	}

	return doc, nil
}

// declLineRange returns the 1-based inclusive line span of a declaration,
// extending the start upward to include any leading doc comment.
func declLineRange(fset *token.FileSet, decl ast.Decl) (int, int) {
	start := fset.Position(decl.Pos()).Line
	end := fset.Position(decl.End()).Line

	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Doc != nil {
			start = fset.Position(d.Doc.Pos()).Line
		}
	case *ast.GenDecl:
		if d.Doc != nil {
			start = fset.Position(d.Doc.Pos()).Line
		}
	default:
		// other decls: keep the node's own start line
	}

	return start, end
}

// declName renders a stable heading path for a declaration.
func declName(decl ast.Decl) string {
	switch d := decl.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			return "method " + receiverName(d.Recv.List[0].Type) + "." + d.Name.Name
		}

		return "func " + d.Name.Name
	case *ast.GenDecl:
		return genDeclName(d)
	default:
		return "decl"
	}
}

// genDeclName names a type/var/const block by its first spec.
func genDeclName(d *ast.GenDecl) string {
	kind := d.Tok.String()
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			return "type " + s.Name.Name
		case *ast.ValueSpec:
			if len(s.Names) > 0 {
				return kind + " " + s.Names[0].Name
			}
		default:
			// other specs fall through to the kind name
		}
	}

	return kind
}

// receiverName extracts the base type name of a method receiver.
func receiverName(expr ast.Expr) string {
	switch e := expr.(type) {
	case *ast.StarExpr:
		return receiverName(e.X)
	case *ast.Ident:
		return e.Name
	default:
		return ""
	}
}

// wholeFileSection builds the parse-failure fallback section.
func wholeFileSection(content []byte) model.Section {
	s := string(content)
	lineCount := strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
	if s == "" {
		lineCount = 1
	}

	return model.Section{StartLine: 1, EndLine: lineCount, Content: s}
}

// sliceLines returns the 1-based inclusive line range [start, end] of content.
func sliceLines(content []byte, start, end int) string {
	lines := strings.Split(string(content), "\n")
	if start < 1 {
		start = 1
	}
	if end > len(lines) {
		end = len(lines)
	}
	if end < start {
		return ""
	}

	return strings.Join(lines[start-1:end], "\n")
}
