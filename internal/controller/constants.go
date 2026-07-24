package controller

const (
	// ApplicationFinalizer is the unique string key attached to metadata.finalizers of Application resources to ensure cleanup of associated resources before deletion.
	ApplicationFinalizer = "forge.ningendo7.github.io/finalizer"
)