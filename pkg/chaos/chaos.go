package chaos

import (
	"context"
	"math/rand"
	"os"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"

	corev1 "k8s.io/api/core/v1"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

// Monkeys knows how to crush pods and nodes.
type Monkeys struct {
	mgr manager.Manager
}

func NewMonkeys(mgr manager.Manager) *Monkeys {
	return &Monkeys{mgr: mgr}
}

type CrashConfig struct {
	Namespace string
	Selector  map[string]string

	KillRate          rate.Limit
	CbKillProbability float64
	OpKillProbability float64
	KillMax           int
	MinPods           int
	logger            *logrus.Entry
}

// TODO: respect context in k8s operations.
func (m *Monkeys) CrushPods(ctx context.Context, c *CrashConfig) {
	cli := m.mgr.GetClient()
	burst := int(c.KillRate)
	if burst <= 0 {
		burst = 1
	}
	limiter := rate.NewLimiter(c.KillRate, burst)
	for {
		err := limiter.Wait(ctx)
		if err != nil { // user cancellation
			c.logger.Infof("crushPods is canceled by the user: %v", err)
			return
		}

		if p := rand.Float64(); p < c.OpKillProbability {
			c.logger.Infof("killing operator pod: probability: %v, got p: %v", c.OpKillProbability, p)
			time.Sleep(5 * time.Second)
			// fare thee well
			os.Exit(0)
		}

		if p := rand.Float64(); p > c.CbKillProbability {
			c.logger.Infof("skip killing pod: probability: %v, got p: %v", c.CbKillProbability, p)
			continue
		}

		pods := &corev1.PodList{}
		err = cli.List(ctx, client.InNamespace(c.Namespace).MatchingLabels(c.Selector), pods)
		if err != nil {
			c.logger.Errorf("failed to list pods for selector %v: %v", c.Selector, err)
			continue
		}
		if len(pods.Items) == 0 {
			c.logger.Infof("no pods to kill for selector %v", c.Selector)
			continue
		}

		max := len(pods.Items)
		kmax := rand.Intn(c.KillMax) + 1
		if kmax < max {
			max = kmax
		}
		if len(pods.Items)-max < c.MinPods {
			max -= 1
			if max == 0 {
				c.logger.Infof("skip killing pod: min pods required %d", c.MinPods)
				continue
			}
		}

		c.logger.Infof("start to kill %d pods for selector %v", max, c.Selector)

		tokills := []*corev1.Pod{}
		for len(tokills) < max {
			tokills = append(tokills, &pods.Items[rand.Intn(len(pods.Items))])
		}

		for _, tokill := range tokills {
			err = cli.Delete(ctx, tokill)
			if err != nil {
				c.logger.Errorf("failed to kill pod %v: %v", tokill.Name, err)
				continue
			}
			c.logger.Infof("killed pod %v for selector %v", tokill.Name, c.Selector)
		}
	}
}

func Start(ctx context.Context, mgr manager.Manager, ns string, chaosLevel int) {
	m := NewMonkeys(mgr)
	ls := map[string]string{"app": "couchbase"}
	logger := logrus.WithField("module", "chaos")

	switch chaosLevel {
	case 1:
		logger.Info("chaos level = 1: randomly kill one couchbase pod every 30 seconds at 50%")
		c := &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:          rate.Every(30 * time.Second),
			CbKillProbability: 0.5,
			OpKillProbability: 0,
			KillMax:           2,
			MinPods:           1,
			logger:            logger,
		}
		go func() {
			time.Sleep(30 * time.Second)
			m.CrushPods(ctx, c)
		}()
	case 2:
		logger.Info("chaos level = 2: randomly kill at most two couchbase pod with at least 1 alive every 2 minutes at 50%")
		c := &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:          rate.Every(120 * time.Second),
			CbKillProbability: 0.5,
			OpKillProbability: 0,
			KillMax:           2,
			MinPods:           1,
			logger:            logger,
		}
		go func() {
			time.Sleep(300 * time.Second)
			m.CrushPods(ctx, c)
		}()
	case 3:
		logger.Info("chaos level = 3: randomly kill at most two couchbase pod every 2 minutes at 50%")
		c := &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:          rate.Every(120 * time.Second),
			CbKillProbability: 0.5,
			OpKillProbability: 0,
			KillMax:           2,
			MinPods:           0,
			logger:            logger,
		}
		go func() {
			time.Sleep(300 * time.Second)
			m.CrushPods(ctx, c)
		}()
	case 4:
		logger.Info("chaos level = 4: randomly kill at most five couchbase pods every 30 seconds at 50%")
		c := &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:          rate.Every(30 * time.Second),
			CbKillProbability: 0.5,
			OpKillProbability: 0,
			KillMax:           5,
			MinPods:           0,
			logger:            logger,
		}
		go func() {
			time.Sleep(300 * time.Second)
			m.CrushPods(ctx, c)
		}()
	case 5:
		logger.Info("chaos level = 5: randomly kill couchbase pods (50%) or operator (20%) every 2 minutes")
		c := &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:          rate.Every(120 * time.Second),
			CbKillProbability: 0.5,
			OpKillProbability: 0.2,
			KillMax:           2,
			MinPods:           0,
			logger:            logger,
		}
		go func() {
			time.Sleep(300 * time.Second)
			m.CrushPods(ctx, c)
		}()

	default:
	}
}
