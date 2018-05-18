package chaos

import (
	"context"
	"math/rand"
	"time"

	"github.com/sirupsen/logrus"
	"golang.org/x/time/rate"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/client-go/kubernetes"
)

// Monkeys knows how to crush pods and nodes.
type Monkeys struct {
	kubecli kubernetes.Interface
}

func NewMonkeys(kubecli kubernetes.Interface) *Monkeys {
	return &Monkeys{kubecli: kubecli}
}

type CrashConfig struct {
	Namespace string
	Selector  labels.Selector

	KillRate        rate.Limit
	KillProbability float64
	KillMax         int
	MinPods         int
}

// TODO: respect context in k8s operations.
func (m *Monkeys) CrushPods(ctx context.Context, c *CrashConfig) {
	burst := int(c.KillRate)
	if burst <= 0 {
		burst = 1
	}
	limiter := rate.NewLimiter(c.KillRate, burst)
	ls := c.Selector.String()
	ns := c.Namespace
	for {
		err := limiter.Wait(ctx)
		if err != nil { // user cancellation
			logrus.Infof("crushPods is canceled for selector %v by the user: %v", ls, err)
			return
		}

		if p := rand.Float64(); p > c.KillProbability {
			logrus.Infof("skip killing pod: probability: %v, got p: %v", c.KillProbability, p)
			continue
		}

		pods, err := m.kubecli.CoreV1().Pods(ns).List(metav1.ListOptions{LabelSelector: ls})
		if err != nil {
			logrus.Errorf("failed to list pods for selector %v: %v", ls, err)
			continue
		}
		if len(pods.Items) == 0 {
			logrus.Infof("no pods to kill for selector %v", ls)
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
				logrus.Infof("skip killing pod: min pods required ", c.MinPods)
				continue
			}
		}

		logrus.Infof("start to kill %d pods for selector %v", max, ls)

		tokills := make(map[string]struct{})
		for len(tokills) < max {
			tokills[pods.Items[rand.Intn(len(pods.Items))].Name] = struct{}{}
		}

		for tokill := range tokills {
			err = m.kubecli.CoreV1().Pods(ns).Delete(tokill, metav1.NewDeleteOptions(0))
			if err != nil {
				logrus.Errorf("failed to kill pod %v: %v", tokill, err)
				continue
			}
			logrus.Infof("killed pod %v for selector %v", tokill, ls)
		}
	}
}

func Start(ctx context.Context, kubecli kubernetes.Interface, ns string, chaosLevel int) {
	m := NewMonkeys(kubecli)
	ls := labels.SelectorFromSet(map[string]string{"app": "couchbase"})

	switch chaosLevel {
	case 1:
		logrus.Info("chaos level = 1: randomly kill one couchbase pod every 30 seconds at 50%")
		c := &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:        rate.Every(30 * time.Second),
			KillProbability: 0.5,
			KillMax:         2,
			MinPods:         1,
		}
		go func() {
			time.Sleep(30 * time.Second)
			m.CrushPods(ctx, c)
		}()
	case 2:
		logrus.Info("chaos level = 2: randomly kill at most two couchbase pod with at least 1 alive every 2 minutes at 50%")
		c := &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:        rate.Every(120 * time.Second),
			KillProbability: 0.5,
			KillMax:         2,
			MinPods:         1,
		}
		go func() {
			time.Sleep(300 * time.Second)
			m.CrushPods(ctx, c)
		}()
	case 3:
		logrus.Info("chaos level = 3: randomly kill at most two couchbase pod every 2 minutes at 50%")
		c := &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:        rate.Every(120 * time.Second),
			KillProbability: 0.5,
			KillMax:         2,
			MinPods:         0,
		}
		go func() {
			time.Sleep(300 * time.Second)
			m.CrushPods(ctx, c)
		}()
	case 4:
		logrus.Info("chaos level = 4: randomly kill at most five couchbase pods every 30 seconds at 50%")
		c := &CrashConfig{
			Namespace: ns,
			Selector:  ls,

			KillRate:        rate.Every(30 * time.Second),
			KillProbability: 0.5,
			KillMax:         5,
			MinPods:         0,
		}

		go m.CrushPods(ctx, c)

	default:
	}
}
