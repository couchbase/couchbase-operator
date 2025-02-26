package task

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/goccy/go-yaml"
)

var (
	ErrActionNil          = errors.New("action is nil")
	ErrFailedToUnmarshal  = errors.New("failed to unmarshal config object")
	ErrNoAction           = errors.New("no action registered")
	ErrRecursionThreshold = errors.New("caught runaway recursion, depth > 100")
	ErrScenarioParser     = errors.New("unable to parse scenario")
	ErrActionConfigFailed = errors.New("failed to configure action")
	ErrNoMultiScenario    = errors.New("zero Scenarios found in file")
)

func (r Register) CreateFromFile(path FilePath, read FileReaderFunc) ([]*ScenarioRunner, error) {
	b, err := read(path)
	if err != nil {
		return nil, err
	}

	var t taskIn

	var ms multiScenarioIn

	var singleFileError error

	var multiFileError error

	if multiFileError = yaml.Unmarshal(b, &ms); multiFileError == nil {
		if len(ms.Scenarios) == 0 {
			multiFileError = ErrNoMultiScenario
		} else {
			return r.createMultiFromFile(read, ms)
		}
	}

	ctx := context.Background()

	if singleFileError = yaml.NewDecoder(bytes.NewReader(b), yaml.Strict()).Decode(&t); singleFileError == nil {
		t.ScenarioName = scenarioNameFromPath(path)
		tree, ctx, err := r.createSingleFromFile(ctx, t)
		singleFileError = err

		if err == nil {
			return []*ScenarioRunner{{tree, ctx}}, err
		}
	}

	return nil, fmt.Errorf("%w: %w : %w", ErrScenarioParser, singleFileError, multiFileError)
}

func (r Register) createMultiFromFile(read FileReaderFunc, ms multiScenarioIn) ([]*ScenarioRunner, error) {
	out := []*ScenarioRunner{}

	// all scenarios in the same set are placed into the same tree so they run concurrently
	for _, set := range ms.Scenarios {
		// only 1 tree and context per scenario set,
		// having a single context can cause trouble with
		// upgrade scenarios since only the last will be used
		ctx := context.Background()

		tree := newTree()

		if ms.TimeoutInMins > 0 {
			tree.Timeout = time.Duration(ms.TimeoutInMins) * time.Minute
		}

		for _, scenario := range set {
			// read then parse the specified scenario
			b, err := read(scenario)
			if err != nil {
				return nil, err
			}

			var t taskIn
			if err := yaml.NewDecoder(bytes.NewReader(b), yaml.Strict()).Decode(&t); err != nil {
				return nil, fmt.Errorf("  %w:%s", err, scenario)
			}

			t.ScenarioName = scenarioNameFromPath(scenario)

			// if there are multiple scenarios, place them all into a parent top level tree
			// the context is different than background only when there is a release
			var subTree *Tree
			subTree, ctx, err = r.createSingleFromFile(ctx, t)

			if err != nil {
				return nil, err
			}

			// add the subtree to the top level tree
			tree.Trees = append(tree.Trees, subTree)
		}

		out = append(out, &ScenarioRunner{tree, ctx})
	}

	return out, nil
}

func (r Register) createSingleFromFile(ctx context.Context, t taskIn) (*Tree, context.Context, error) {
	out := newTree()
	if err := r.populate(t, out, 0); err != nil {
		return nil, nil, err
	}

	return out, ctx, nil
}

func (r Register) populate(task taskIn, out *Tree, depth int) error {
	depth++
	if depth > maxDepth {
		return ErrRecursionThreshold
	}

	if task.MaxConcurrent > 1 {
		out.MaxConcurrent = task.MaxConcurrent
	}

	if task.Iterations > 1 {
		out.Iterations = task.Iterations
	}

	if task.TimeoutInMins > 0 {
		out.Timeout = time.Duration(task.TimeoutInMins) * time.Minute
	}

	out.Blocking = task.Blocking
	out.ScenarioName = task.ScenarioName
	out.Validators = task.Validators

	if err := r.createAction(task, out); err != nil {
		return err
	}

	for _, v := range task.Trees {
		subtree := newTree()
		out.Trees = append(out.Trees, subtree)
		v.ScenarioName = task.ScenarioName

		if err := r.populate(v, subtree, depth); err != nil {
			return err
		}
	}

	return nil
}

func (r Register) createAction(task taskIn, out *Tree) error {
	if task.Action != "" {
		if a, ok := r.Actions()[task.Action]; ok {
			// unmarshal the config onto the given config object
			var err error

			switch {
			case task.Config != nil:
				var encoded []byte

				c := a.config
				encoded, err = yaml.Marshal(task.Config)

				if err != nil {
					return fmt.Errorf("%w: %w", ErrFailedToUnmarshal, err)
				}

				if err := yaml.NewDecoder(bytes.NewReader(encoded), yaml.Strict()).Decode(c); err != nil {
					return fmt.Errorf("%w: %s", ErrFailedToUnmarshal, err.Error())
				}

				out.Action, err = a.newAction(c)
			default:
				out.Action, err = a.newAction(nil)
			}

			if err != nil {
				return fmt.Errorf("%w: %w", ErrActionConfigFailed, err)
			}

			if out.Action == nil {
				return fmt.Errorf("%w: %s", ErrActionNil, task.Action)
			}
		} else {
			return fmt.Errorf("%w: '%s'", ErrNoAction, task.Action)
		}
	}

	return nil
}
