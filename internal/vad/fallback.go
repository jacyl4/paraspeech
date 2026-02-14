package vad

// ExtractSegments extracts speech samples from the full audio based on segments.
func ExtractSegments(allSamples []int16, segments []AudioSegment) []int16 {
	if len(segments) == 0 {
		return nil
	}
	total := 0
	for _, s := range segments {
		end := s.EndSample
		if end > len(allSamples) {
			end = len(allSamples)
		}
		start := s.StartSample
		if start > len(allSamples) {
			start = len(allSamples)
		}
		total += end - start
	}
	result := make([]int16, 0, total)
	for _, s := range segments {
		end := s.EndSample
		if end > len(allSamples) {
			end = len(allSamples)
		}
		start := s.StartSample
		if start > len(allSamples) {
			continue
		}
		result = append(result, allSamples[start:end]...)
	}
	return result
}
