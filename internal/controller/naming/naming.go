// Package naming is the single source of truth for names of resources the
// Application controller creates, so builders and readers never disagree.
package naming

import (
	forgev1alpha1 "github.com/Ningendo7/forge-operator/api/v1alpha1"
)

// Service returns the name of the Application's Service.
func Service(application *forgev1alpha1.Application) string {
	return application.Name
}

// Deployment returns the name of the Application's Deployment.
func Deployment(application *forgev1alpha1.Application) string {
	return application.Name + "-deployment"
}

// Ingress returns the name of the Application's Ingress.
func Ingress(application *forgev1alpha1.Application) string {
	return application.Name
}

// HPA returns the name of the Application's HorizontalPodAutoscaler.
func HPA(application *forgev1alpha1.Application) string {
	return application.Name + "-hpa"
}

// PDB returns the name of the Application's PodDisruptionBudget.
func PDB(application *forgev1alpha1.Application) string {
	return application.Name + "-pdb"
}
