package il

import (
	"testing"

	"github.com/hashicorp/terraform/config"
	"github.com/hashicorp/terraform/config/module"
	"github.com/stretchr/testify/assert"
)

func newLocal(t *testing.T, name, value string) *config.Local {
	raw, err := config.NewRawConfig(map[string]interface{}{
		"value": value,
	})
	if err != nil {
		t.Fatalf("NewRawConfig failed: %v", err)
	}
	return &config.Local{
		Name:      name,
		RawConfig: raw,
	}
}

func TestCircularLocals(t *testing.T) {
	cfg := &config.Config{
		Locals: []*config.Local{newLocal(t, "a", "${local.a}")},
	}
	tree := module.NewTree("test", cfg)

	_, err := BuildGraph(tree, nil)
	assert.Error(t, err)

	cfg.Locals = []*config.Local{
		newLocal(t, "a", "${local.b}"),
		newLocal(t, "b", "${local.a}"),
	}

	_, err = BuildGraph(tree, nil)
	assert.Error(t, err)
}

func TestLocalForwardReferences(t *testing.T) {
	cfg := &config.Config{
		Locals: []*config.Local{
			newLocal(t, "a", "${local.b}"),
			newLocal(t, "b", "foo"),
		},
	}
	tree := module.NewTree("test", cfg)

	_, err := BuildGraph(tree, nil)
	assert.NoError(t, err)
}
