package container

import (
	"testing"

	"go.uber.org/dig"
)

func TestDocumentRuntimeDependencyGraphHasNoAgentSessionCycle(t *testing.T) {
	t.Helper()
	// DryRun validates all constructors/Invoke dependencies without starting
	// databases, workers, HTTP listeners or other service side effects.
	BuildContainer(dig.New(dig.DryRun(true)))
}
