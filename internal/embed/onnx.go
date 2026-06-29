//go:build embed

package embed

import (
	"context"
	"fmt"
	"path/filepath"

	"github.com/gomlx/gomlx/backends"
	// Blank import registers the pure-Go "SimpleGo" backend under the name "go".
	_ "github.com/gomlx/gomlx/backends/simplego"
	"github.com/gomlx/gomlx/pkg/core/graph"
	"github.com/gomlx/gomlx/pkg/core/tensors"
	mlcontext "github.com/gomlx/gomlx/pkg/ml/context"
	"github.com/gomlx/onnx-gomlx/onnx"
	"github.com/gomlx/onnx-gomlx/onnx/parser"
	"github.com/sugarme/tokenizer"
	"github.com/sugarme/tokenizer/pretrained"
)

// Supported reports that this binary was built with semantic-embedding support.
const Supported = true

// onnxEmbedder runs all-MiniLM-L6-v2 on the pure-Go SimpleGo backend. Pooling
// and L2 normalization are done in plain Go (MeanPool/L2Normalize) so SimpleGo
// only has to execute the transformer's token-embedding graph. The struct is not
// safe for concurrent Embed calls; callers serialize batches.
type onnxEmbedder struct {
	backend    backends.Backend
	ctx        *mlcontext.Context
	model      onnx.Model
	tk         *tokenizer.Tokenizer
	exec       *mlcontext.Exec
	inputNames []string
	outputName string
}

// New loads model.onnx and tokenizer.json from modelDir and prepares the
// SimpleGo execution graph. It forces the pure-Go backend regardless of any
// GOMLX_BACKEND environment variable, keeping the binary cgo-free.
func New(modelDir string) (Embedder, error) {
	backend, err := backends.NewWithConfig("go")
	if err != nil {
		return nil, fmt.Errorf("embed: create SimpleGo backend: %w", err)
	}

	tk, err := pretrained.FromFile(filepath.Join(modelDir, "tokenizer.json"))
	if err != nil {
		return nil, fmt.Errorf("embed: load tokenizer: %w", err)
	}

	model, err := parser.ParseFile(filepath.Join(modelDir, "model.onnx"))
	if err != nil {
		return nil, fmt.Errorf("embed: parse onnx model: %w", err)
	}

	inputNames, _ := model.Inputs()
	outputNames, _ := model.Outputs()
	if len(outputNames) == 0 {
		return nil, fmt.Errorf("embed: model has no outputs")
	}

	ctx := mlcontext.New()
	if err := model.VariablesToContext(ctx); err != nil {
		return nil, fmt.Errorf("embed: load variables: %w", err)
	}

	e := &onnxEmbedder{
		backend:    backend,
		ctx:        ctx,
		model:      model,
		tk:         tk,
		inputNames: inputNames,
		outputName: outputNames[0],
	}

	graphFn := func(ctx *mlcontext.Context, inputs []*graph.Node) []*graph.Node {
		inMap := make(map[string]*graph.Node, len(inputs))
		for i, name := range e.inputNames {
			inMap[name] = inputs[i]
		}
		return e.model.CallGraph(ctx, inputs[0].Graph(), inMap, e.outputName)
	}

	exec, err := mlcontext.NewExec(backend, ctx, graphFn)
	if err != nil {
		return nil, fmt.Errorf("embed: build exec: %w", err)
	}
	e.exec = exec
	return e, nil
}

// Dim reports the embedding dimensionality.
func (e *onnxEmbedder) Dim() int { return Dim }

// Model reports the model name persisted with each vector.
func (e *onnxEmbedder) Model() string { return DefaultModel }

// Embed returns one L2-normalized Dim-length vector per input text. The context
// is checked before the (synchronous, CPU-bound) forward pass so a cancelled
// batch returns promptly.
func (e *onnxEmbedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	ids, mask, typ, batch, seqLen, err := e.tokenize(texts)
	if err != nil {
		return nil, err
	}

	idsT := tensors.FromFlatDataAndDimensions(ids, batch, seqLen)
	maskT := tensors.FromFlatDataAndDimensions(mask, batch, seqLen)
	typT := tensors.FromFlatDataAndDimensions(typ, batch, seqLen)

	args := make([]any, len(e.inputNames))
	for i, name := range e.inputNames {
		switch name {
		case "input_ids":
			args[i] = idsT
		case "attention_mask":
			args[i] = maskT
		case "token_type_ids":
			args[i] = typT
		default:
			return nil, fmt.Errorf("embed: unexpected model input %q", name)
		}
	}

	outs, err := e.exec.Exec(args...)
	if err != nil {
		return nil, fmt.Errorf("embed: run graph: %w", err)
	}

	out := outs[0]
	dims := out.Shape().Dimensions
	if len(dims) != 3 || dims[2] != Dim {
		return nil, fmt.Errorf("embed: unexpected output shape %v", dims)
	}

	var flat []float32
	if err := tensors.ConstFlatData(out, func(d []float32) {
		flat = make([]float32, len(d))
		copy(flat, d)
	}); err != nil {
		return nil, fmt.Errorf("embed: read output: %w", err)
	}

	vecs := MeanPool(flat, mask, batch, seqLen, Dim)
	for _, v := range vecs {
		L2Normalize(v)
	}
	return vecs, nil
}

// tokenize encodes texts and pads each to the batch's longest sequence,
// returning int64 row-major flat slices for ids, attention mask and type ids.
func (e *onnxEmbedder) tokenize(texts []string) (ids, mask, typ []int64, batch, seqLen int, err error) {
	batch = len(texts)
	encIds := make([][]int, batch)
	encMask := make([][]int, batch)
	encTyp := make([][]int, batch)
	for i, t := range texts {
		enc, encErr := e.tk.EncodeSingle(t, true)
		if encErr != nil {
			return nil, nil, nil, 0, 0, fmt.Errorf("embed: tokenize: %w", encErr)
		}
		encIds[i] = enc.GetIds()
		encMask[i] = enc.GetAttentionMask()
		encTyp[i] = enc.GetTypeIds()
		if len(encIds[i]) > seqLen {
			seqLen = len(encIds[i])
		}
	}

	ids = make([]int64, batch*seqLen)
	mask = make([]int64, batch*seqLen)
	typ = make([]int64, batch*seqLen)
	for i := 0; i < batch; i++ {
		for j := 0; j < len(encIds[i]); j++ {
			off := i*seqLen + j
			ids[off] = int64(encIds[i][j])
			mask[off] = int64(encMask[i][j])
			typ[off] = int64(encTyp[i][j])
		}
	}
	return ids, mask, typ, batch, seqLen, nil
}
