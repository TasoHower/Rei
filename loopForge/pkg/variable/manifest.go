package variable

// Spec describes one variable in a static manifest (schema for Materialize).
type Spec struct {
	Key         string
	Description string
	// Visitable is nil => true.
	Visitable *bool
	Default   any
}

func specVisitable(s Spec) bool {
	if s.Visitable == nil {
		return true
	}
	return *s.Visitable
}

// Manifest is a list of variable specs for an application.
type Manifest struct {
	Specs []Spec
}

type materializeConfig struct {
	dropOrphans bool
}

// MaterializeOption configures Materialize.
type MaterializeOption func(*materializeConfig)

// WithDropOrphans removes keys not present in manifest (except runtime keys).
func WithDropOrphans(drop bool) MaterializeOption {
	return func(c *materializeConfig) {
		c.dropOrphans = drop
	}
}

// Materialize merges persisted snapshot, manifest schema, and runtime bindings.
func Materialize(m *Manifest, persisted *StoreSnapshot, runtime map[string]any, opts ...MaterializeOption) *VarStore {
	var cfg materializeConfig
	for _, o := range opts {
		if o != nil {
			o(&cfg)
		}
	}

	var base *VarStore
	if persisted != nil {
		base = Import(persisted)
	} else {
		base = New()
	}

	manifestKeys := map[string]bool{}
	if m != nil {
		for _, spec := range m.Specs {
			if spec.Key == "" {
				continue
			}
			manifestKeys[spec.Key] = true
			vis := specVisitable(spec)
			ent, ok := base.GetEntry(spec.Key)
			if !ok {
				if spec.Default != nil {
					base.Set(spec.Key, spec.Default, WithDescription(spec.Description), WithVisitable(vis))
				} else {
					base.Define(spec.Key, spec.Description, WithVisitable(vis))
				}
				continue
			}
			// Patch metadata; keep existing value (including nil).
			desc := ent.Description
			if spec.Description != "" {
				desc = spec.Description
			}
			visVal := ent.Visitable
			if spec.Visitable != nil {
				visVal = vis
			}
			if ent.Value == nil {
				base.Define(spec.Key, desc, WithVisitable(visVal))
			} else {
				base.Set(spec.Key, ent.Value, WithDescription(desc), WithVisitable(visVal))
			}
		}
	}

	runtimeKeys := map[string]bool{}
	for k := range runtime {
		runtimeKeys[k] = true
	}
	for k, v := range runtime {
		base.Set(k, v)
	}

	if cfg.dropOrphans && m != nil && len(m.Specs) > 0 {
		for _, k := range base.Keys() {
			if !manifestKeys[k] && !runtimeKeys[k] {
				base.Delete(k)
			}
		}
	}

	return base
}
