package embed

// MeanPool applies attention-mask-weighted mean pooling over the token axis of a
// transformer's last hidden state. flat is the row-major [batch, seqLen, dim]
// output; mask is the row-major [batch, seqLen] attention mask (1 = real token,
// 0 = padding). It returns one unnormalized dim-length vector per batch row: the
// average of the hidden states of the unmasked tokens. Rows with no unmasked
// tokens yield a zero vector. The result is not L2-normalized; call L2Normalize.
func MeanPool(flat []float32, mask []int64, batch, seqLen, dim int) [][]float32 {
	out := make([][]float32, batch)
	for b := range batch {
		vec := make([]float32, dim)
		var count float32
		for t := range seqLen {
			if mask[b*seqLen+t] == 0 {
				continue
			}
			count++
			base := (b*seqLen + t) * dim
			for d := range dim {
				vec[d] += flat[base+d]
			}
		}
		if count > 0 {
			for d := range dim {
				vec[d] /= count
			}
		}
		out[b] = vec
	}

	return out
}
