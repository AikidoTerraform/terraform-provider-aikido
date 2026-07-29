package helpers

import "slices"

func NormalizeIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}

	return ids
}

func DroppedRepoIDs(planned []int64, actual []int64) []int64 {
	actualSet := make(map[int64]struct{}, len(actual))
	for _, id := range actual {
		actualSet[id] = struct{}{}
	}

	var dropped []int64
	seen := make(map[int64]struct{}, len(planned))
	for _, id := range planned {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}

		if _, ok := actualSet[id]; !ok {
			dropped = append(dropped, id)
		}
	}

	slices.Sort(dropped)
	return dropped
}
