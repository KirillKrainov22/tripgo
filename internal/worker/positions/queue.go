package positions

type taskHeap []task

func (h taskHeap) Len() int {
	return len(h)
}

func (h taskHeap) Less(i, j int) bool {
	if !h[i].trip.LastSeenAt.Equal(h[j].trip.LastSeenAt) {
		return h[i].trip.LastSeenAt.Before(h[j].trip.LastSeenAt)
	}
	return h[i].token < h[j].token
}

func (h taskHeap) Swap(i, j int) {
	h[i], h[j] = h[j], h[i]
}

func (h *taskHeap) Push(value any) {
	if next, ok := value.(task); ok {
		*h = append(*h, next)
	}
}

func (h *taskHeap) Pop() any {
	old := *h
	last := old[len(old)-1]
	*h = old[:len(old)-1]
	return last
}
