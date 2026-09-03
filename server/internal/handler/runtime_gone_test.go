package handler

type recordingRuntimeGoneNotifier struct {
	runtimeIDs []string
}

func (n *recordingRuntimeGoneNotifier) NotifyRuntimeGone(runtimeID string) {
	n.runtimeIDs = append(n.runtimeIDs, runtimeID)
}
