package job

import (
	"github.com/sirupsen/logrus"
	batchv1 "k8s.io/api/batch/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes"
)

// Scheduler runs jobs against pods and handles constraints required
// to run jobs for coordination.
type Scheduler struct {

	// context for scheduling jobs
	context KubeContext

	// history of jobs that have run against a pod
	// this allows handling of scheduling restraints
	// where a job cannot be scheduled if a required
	// action has not occurred.
	//ie... cannot create-bucket without node-init
	history map[string][]JobType

	// this channel can be watched to track status
	// of jobs running against a pod
	StatusCh chan JobStatus

	logger *logrus.Entry
}

func NewScheduler(cli kubernetes.Interface, ns string) *Scheduler {
	return &Scheduler{
		context: KubeContext{
			KubeCli:   cli,
			Namespace: ns,
		},
		history:  make(map[string][]JobType),
		StatusCh: make(chan JobStatus),
		logger: logrus.WithFields(logrus.Fields{
			"module": "scheduler",
		}),
	}
}

// Dispatch runs jobs.  each job is set to be watched
func (s *Scheduler) Dispatch(jobs []JobRunner) error {
	for _, job := range jobs {
		target := job.GetTarget()
		s.history[target] = []JobType{} /* TODO: remember to delete! */
		err := s.dispatch(job)
		if err != nil {
			// TODO: handle issue with starting jobs
			return err
		}
	}
	return nil
}

// dispatch runs a single job and watches
// when job finishes in order to write into
// its status chan
func (s *Scheduler) dispatch(job JobRunner) error {
	// make sure any tasks this job requires have already completed

	target := job.GetTarget()
	s.history[target] = append(s.history[target], job.GetType())

	// prepare watcher
	selector := "op=" + job.GetType().Str()
	watcher, err := s.context.
		KubeCli.
		BatchV1().
		Jobs(s.context.Namespace).
		Watch(metav1.ListOptions{LabelSelector: selector})

	if err != nil {
		return err
	}

	go func() {
		for event := range watcher.ResultChan() {
			evType := event.Type
			switch evType {
			case watch.Added:
				s.logger.Info("job started")
			case watch.Modified:
				evJob := event.Object.(*batchv1.Job)
				s.logger.Infof("job modified %d", evJob.Status.Succeeded)
				if evJob.Status.Succeeded == 1 {
					// Job succeeded, notify
					s.StatusCh <- JobStatus{
						Type:  JobType(evJob.Name),
						Phase: Completed}
				}
			case watch.Deleted:
				s.logger.Info("job deleted")
			case watch.Error:
				s.logger.Errorf("job Error: %v", event.Object)
			}
		}
	}()

	return job.Run(s.context)
}
