package main

import (
	"sync"
	"time"
)

// ProgressReport is a snapshot of scan progress.
type ProgressReport struct {
	ScanID                 string                     `json:"scan_id"`
	StartTime              time.Time                  `json:"start_time"`
	CurrentTime            time.Time                  `json:"current_time"`
	ElapsedSeconds         float64                    `json:"elapsed_seconds"`
	TotalServices          int                        `json:"total_services"`
	CompletedServices      int                        `json:"completed_services"`
	ResourcesFound         int                        `json:"resources_found"`
	ServicesProgress       map[string]ServiceProgress `json:"services_progress"`
	EstimatedTimeRemaining float64                    `json:"estimated_time_remaining_seconds"`
	PercentComplete        float64                    `json:"percent_complete"`
}

type ServiceProgress struct {
	ServiceName   string    `json:"service_name"`
	Status        string    `json:"status"` // pending, running, completed, failed
	StartTime     time.Time `json:"start_time,omitempty"`
	EndTime       time.Time `json:"end_time,omitempty"`
	ResourceCount int       `json:"resource_count"`
	Error         string    `json:"error,omitempty"`
}

type ScanProgressTracker struct {
	mu               sync.RWMutex
	scanID           string
	startTime        time.Time
	totalServices    int
	servicesProgress map[string]*ServiceProgress
	resourcesFound   int
}

func NewScanProgressTracker(scanID string, services []string) *ScanProgressTracker {
	t := &ScanProgressTracker{
		scanID:           scanID,
		startTime:        time.Now(),
		totalServices:    len(services),
		servicesProgress: make(map[string]*ServiceProgress, len(services)),
	}
	for _, svc := range services {
		t.servicesProgress[svc] = &ServiceProgress{ServiceName: svc, Status: "pending"}
	}
	return t
}

func (t *ScanProgressTracker) StartService(name string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p, ok := t.servicesProgress[name]; ok {
		p.Status = "running"
		p.StartTime = time.Now()
	}
}

func (t *ScanProgressTracker) CompleteService(name string, count int, err error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if p, ok := t.servicesProgress[name]; ok {
		if err != nil {
			p.Status = "failed"
			p.Error = err.Error()
		} else {
			p.Status = "completed"
		}
		p.EndTime = time.Now()
		p.ResourceCount = count
		t.resourcesFound += count
	}
}

func (t *ScanProgressTracker) GetProgressReport() *ProgressReport {
	t.mu.RLock()
	defer t.mu.RUnlock()

	now := time.Now()
	elapsed := now.Sub(t.startTime).Seconds()
	completed := 0
	progress := make(map[string]ServiceProgress, len(t.servicesProgress))
	for k, v := range t.servicesProgress {
		progress[k] = *v
		if v.Status == "completed" || v.Status == "failed" {
			completed++
		}
	}
	pct := 0.0
	if t.totalServices > 0 {
		pct = float64(completed) / float64(t.totalServices) * 100
	}
	eta := 0.0
	if completed > 0 && completed < t.totalServices {
		avg := elapsed / float64(completed)
		eta = avg * float64(t.totalServices-completed)
	}
	return &ProgressReport{
		ScanID:                 t.scanID,
		StartTime:              t.startTime,
		CurrentTime:            now,
		ElapsedSeconds:         elapsed,
		TotalServices:          t.totalServices,
		CompletedServices:      completed,
		ResourcesFound:         t.resourcesFound,
		ServicesProgress:       progress,
		EstimatedTimeRemaining: eta,
		PercentComplete:        pct,
	}
}
