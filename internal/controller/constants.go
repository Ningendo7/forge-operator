package controller

const (
	// ApplicationFinalizer ensures cleanup before an Application is deleted.
	ApplicationFinalizer = "forge.ningendo7.github.io/finalizer"

	// appLabelKey is the standard "app" label applied to resources owned by an Application.
	appLabelKey = "app"

	// deploymentKind is the Kind used when referencing a Deployment (e.g. ownerRef, scaleTargetRef).
	deploymentKind = "Deployment"

	// providerStatic is a storage provider value handled as a no-op (credentials supplied externally).
	providerStatic = "Static"
)
