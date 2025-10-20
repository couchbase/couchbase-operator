package chaos

import (
	"context"
	"math/rand"
	"os"
	"time"

	"golang.org/x/time/rate"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"

	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

var log = logf.Log.WithName("chaos")

// Monkeys knows how to crush pods and nodes.
type Monkeys struct {
	mgr manager.Manager
}

func NewMonkeys(mgr manager.Manager) *Monkeys {
	return &Monkeys{mgr: mgr}
}

type CrashConfig struct {
	Namespace string
	Selector  labels.Set

	KillRate          rate.Limit
	CbKillProbability float64
	OpKillProbability float64
	KillMax           int
	MinPods           int
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
			log.Error(err, "crushPods is canceled by the user", "namespace", c.Namespace)
			return
		}

		if p := rand.Float64(); p < c.OpKillProbability {
			log.Info("Killing operator pod", "cluster", c.Namespace, "value", p, "threshold", c.OpKillProbability)
			time.Sleep(5 * time.Second)
			// fare thee well
			os.Exit(0)
		}

		if p := rand.Float64(); p > c.CbKillProbability {
			log.Info("Skipping pod deletion", "cluster", c.Namespace, "value", p, "threshold", c.CbKillProbability)
			continue
		}

		pods := &corev1.PodList{}
		options := &client.ListOptions{
			Namespace:     c.Namespace,
			LabelSelector: c.Selector.AsSelector(),
		}

		if err := cli.List(ctx, pods, options); err != nil {
			log.Error(err, "failed to list pods", "cluster", c.Namespace, "selector", c.Selector.String())
			continue
		}

		if len(pods.Items) == 0 {
			log.Info("No pods listed", "cluster", c.Namespace, "selector", c.Selector.String())
			continue
		}

		max := len(pods.Items)

		kmax := rand.Intn(c.KillMax) + 1

		if kmax < max {
			max = kmax
		}

		if len(pods.Items)-max < c.MinPods {
			max--
			if max == 0 {
				log.Info("Too few pods to kill", "cluster", c.Namespace, "minimum_alive", c.MinPods, "pods_total", len(pods.Items), "pods_to_kill", max)
				continue
			}
		}

		log.Info("Killing pods", "cluster", c.Namespace, "number", max, "selector", c.Selector.String())

		tokills := []*corev1.Pod{}
		for len(tokills) < max {
			tokills = append(tokills, &pods.Items[rand.Intn(len(pods.Items))])
		}

		for _, tokill := range tokills {
			if err := cli.Delete(ctx, tokill); err != nil {
				log.Error(err, "Failed to kill pod", "cluster", tokill.Labels["cluster"], "name", tokill.Name)
				continue
			}

			log.Info("Killed pod", "cluster", tokill.Labels["cluster"], "name", tokill.Name, "selector", c.Selector.String())
		}
	}
}

func Start(ctx context.Context, mgr manager.Manager, ns string, chaosLevel int) {
	m := NewMonkeys(mgr)
	ls := labels.Set{"app": "couchbase"}

	var c *CrashConfig

	var wait time.Duration

	switch chaosLevel {
	case 1:
		c = &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:          rate.Every(30 * time.Second),
			CbKillProbability: 0.5,
			OpKillProbability: 0,
			KillMax:           2,
			MinPods:           1,
		}
		wait = 30 * time.Second
	case 2:
		c = &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:          rate.Every(120 * time.Second),
			CbKillProbability: 0.5,
			OpKillProbability: 0,
			KillMax:           2,
			MinPods:           1,
		}
		wait = 300 * time.Second
	case 3:
		c = &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:          rate.Every(120 * time.Second),
			CbKillProbability: 0.5,
			OpKillProbability: 0,
			KillMax:           2,
			MinPods:           0,
		}
		wait = 300 * time.Second
	case 4:
		c = &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:          rate.Every(30 * time.Second),
			CbKillProbability: 0.5,
			OpKillProbability: 0,
			KillMax:           5,
			MinPods:           0,
		}
		wait = 300 * time.Second
	case 5:
		c = &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:          rate.Every(120 * time.Second),
			CbKillProbability: 0.5,
			OpKillProbability: 0.2,
			KillMax:           2,
			MinPods:           0,
		}
		wait = 300 * time.Second
	}

	if c != nil {
		log.Info("Unleashing chaos monkies.", "cluster", c.Namespace, "rate", c.KillRate, "pod_threshold", c.CbKillProbability, "operator_threshold", c.OpKillProbability, "max_kills", c.KillMax, "min_pods", c.MinPods)

		go func() {
			time.Sleep(wait)
			m.CrushPods(ctx, c)
		}()
	}
}
