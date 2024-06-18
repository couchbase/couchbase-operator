package task

import (
	"context"
	"errors"
	"os"
	"os/signal"
	"syscall"

	"github.com/sirupsen/logrus"
)

var (
	ErrKilled  = errors.New("killed")
	ErrTimeout = errors.New("timeout")
)

func RunScenario(f FilePath, read FileReaderFunc) []error {
	var errs []error

	r := Register{}

	scenarios, err := r.CreateFromFile(f, read)

	if err != nil {
		return []error{err}
	}

	for _, s := range scenarios {
		e := runner(s)
		if e != nil {
			errs = append(errs, e...)
		}
	}

	return errs
}

func runner(s *ScenarioRunner) []error {
	ctx, cancel := context.WithTimeout(s.Ctx, s.Tree.Timeout)
	ctxExec, cancelExec := context.WithCancel(context.Background())
	c := make(chan os.Signal, 1)

	signal.Notify(c, syscall.SIGINT, syscall.SIGTERM)

	defer func() {
		signal.Stop(c)
		cancel()
	}()

	errorsChan := make(chan []error)

	go func() {
		errorsChan <- s.Tree.Execute(ctx)
		close(errorsChan)
		cancelExec()
	}()

	var errs []error

	select {
	// tree timeout hit
	case <-ctx.Done():
		errs = []error{ErrTimeout}
	// killed by ctrl c
	case <-c:
		errs = append(errs, ErrKilled)
	// tree finished executing
	case treeErrors := <-errorsChan:
		errs = treeErrors
	// tree finishes execution backup
	case <-ctxExec.Done():
		break
	}

	if len(errs) > 0 {
		logrus.Error(errs)
	}

	return errs
}
