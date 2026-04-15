package ingest

type Chunk struct {
	Content  string
	Index    int
	Metadata map[string]string
}

func ChunkText(text string, chunkSize, overlap int, metadata map[string]string) []Chunk {
	if text == "" || chunkSize <= 0 {
		return nil
	}
	if overlap >= chunkSize {
		overlap = chunkSize / 4
	}

	runes := []rune(text)
	var chunks []Chunk
	idx := 0
	start := 0

	for start < len(runes) {
		end := start + chunkSize
		if end > len(runes) {
			end = len(runes)
		}
		chunks = append(chunks, Chunk{
			Content:  string(runes[start:end]),
			Index:    idx,
			Metadata: metadata,
		})
		idx++
		if end == len(runes) {
			break
		}
		start = end - overlap
	}
	return chunks
}
