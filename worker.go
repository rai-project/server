package server

import (
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/rai-project/model"
	"github.com/rai-project/uuid"
)

type WorkRequest struct {
	model.JobRequest
}

type Worker struct {
	ID          string
	Work        chan WorkRequest
	WorkerQueue chan chan WorkRequest
	QuitChan    chan bool
}

type Dispatcher struct {
	workers     []*Worker
	workerQueue chan chan WorkRequest
	workQueue   chan WorkRequest
	waitgroup   sync.WaitGroup
}

func NewWorker(id int, workerQueue chan chan WorkRequest) *Worker {
	// Create, and return the worker.
	worker := &Worker{
		ID:          uuid.NewV4() + ":::" + strconv.Itoa(id),
		Work:        make(chan WorkRequest),
		WorkerQueue: workerQueue,
		QuitChan:    make(chan bool),
	}

	return worker
}

func (w *Worker) Start() {
	go func() {
		for {
			// Add ourselves into the worker queue.
			w.WorkerQueue <- w.Work

			select {
			case work := <-w.Work:
				// Receive a work request.
				fmt.Printf("worker%v: Received work request\n", w.ID)

				time.Sleep(time.Second)
				fmt.Printf("worker%v: Hello, %s!\n", w.ID, work.User.Username)

			case <-w.QuitChan:
				// We have been asked to stop.
				fmt.Printf("worker%d stopping\n", w.ID)
				return
			}
		}
	}()
}
func (w *Worker) Stop() {
	go func() {
		w.QuitChan <- true
	}()
}

func StartDispatcher(nworkers int) *Dispatcher {
	// First, initialize the channel we are going to but the workers' work channels into.
	workerQueue := make(chan chan WorkRequest, nworkers)
	workQueue := make(chan WorkRequest, 100)
	workers := make([]*Worker, nworkers)
	var wg sync.WaitGroup

	// Now, create all of our workers.
	for i := 0; i < nworkers; i++ {
		fmt.Println("Starting worker", i+1)
		worker := NewWorker(i+1, workerQueue)
		worker.Start()

		workers[i] = worker
	}

	go func() {
		for {
			select {
			case work := <-workQueue:
				wg.Add(1)
				fmt.Println("queue work requeust")
				worker := <-workerQueue
				go func() {
					defer wg.Done()

					fmt.Println("Dispatching work request")
					worker <- work
				}()
			}
		}
	}()
	return &Dispatcher{
		workers:     workers,
		workQueue:   workQueue,
		workerQueue: workerQueue,
		waitgroup:   wg,
	}
}

func (d *Dispatcher) Stop() {
	d.waitgroup.Wait()
	for _, w := range d.workers {
		w.Stop()
	}
}
