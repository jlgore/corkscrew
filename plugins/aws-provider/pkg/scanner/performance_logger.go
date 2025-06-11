package scanner

import (
	"log"
	"sync"
	"time"
)

// PerformanceLogger logs performance metrics
type PerformanceLogger struct {
	mu                 sync.RWMutex
	operationMetrics   map[string]*OperationMetric
	thresholds         PerformanceThresholds
	enableDetailedLogs bool
}

// OperationMetric tracks metrics for a specific operation
type OperationMetric struct {
	Count       int64
	TotalTime   time.Duration
	MinTime     time.Duration
	MaxTime     time.Duration
	LastTime    time.Duration
	ErrorCount  int64
}

// PerformanceThresholds defines alerting thresholds
type PerformanceThresholds struct {
	SlowOperationThreshold time.Duration
	ErrorRateThreshold     float64
}

// NewPerformanceLogger creates a new performance logger
func NewPerformanceLogger(detailed bool) *PerformanceLogger {
	return &PerformanceLogger{
		operationMetrics:   make(map[string]*OperationMetric),
		enableDetailedLogs: detailed,
		thresholds: PerformanceThresholds{
			SlowOperationThreshold: 5 * time.Second,
			ErrorRateThreshold:     0.1, // 10% error rate
		},
	}
}

// StartOperation starts timing an operation
func (p *PerformanceLogger) StartOperation(opName string) func(error) {
	startTime := time.Now()
	
	return func(err error) {
		duration := time.Since(startTime)
		p.recordOperation(opName, duration, err)
		
		if p.enableDetailedLogs && duration > p.thresholds.SlowOperationThreshold {
			log.Printf("PERF WARNING: Slow operation %s took %v", opName, duration)
		}
	}
}

// recordOperation records metrics for an operation
func (p *PerformanceLogger) recordOperation(opName string, duration time.Duration, err error) {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	metric, exists := p.operationMetrics[opName]
	if !exists {
		metric = &OperationMetric{
			MinTime: duration,
			MaxTime: duration,
		}
		p.operationMetrics[opName] = metric
	}
	
	metric.Count++
	metric.TotalTime += duration
	metric.LastTime = duration
	
	if duration < metric.MinTime {
		metric.MinTime = duration
	}
	if duration > metric.MaxTime {
		metric.MaxTime = duration
	}
	
	if err != nil {
		metric.ErrorCount++
	}
}

// LogSummary logs a performance summary
func (p *PerformanceLogger) LogSummary() {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	log.Println("=== Performance Summary ===")
	
	for opName, metric := range p.operationMetrics {
		avgTime := time.Duration(0)
		if metric.Count > 0 {
			avgTime = metric.TotalTime / time.Duration(metric.Count)
		}
		
		errorRate := float64(metric.ErrorCount) / float64(metric.Count)
		
		log.Printf("Operation: %s", opName)
		log.Printf("  Count: %d, Errors: %d (%.2f%%)", metric.Count, metric.ErrorCount, errorRate*100)
		log.Printf("  Times - Min: %v, Avg: %v, Max: %v, Last: %v", 
			metric.MinTime, avgTime, metric.MaxTime, metric.LastTime)
		
		if errorRate > p.thresholds.ErrorRateThreshold {
			log.Printf("  WARNING: High error rate!")
		}
	}
}

// GetMetrics returns a copy of current metrics
func (p *PerformanceLogger) GetMetrics() map[string]OperationMetric {
	p.mu.RLock()
	defer p.mu.RUnlock()
	
	result := make(map[string]OperationMetric)
	for k, v := range p.operationMetrics {
		result[k] = *v
	}
	return result
}

// Reset clears all metrics
func (p *PerformanceLogger) Reset() {
	p.mu.Lock()
	defer p.mu.Unlock()
	
	p.operationMetrics = make(map[string]*OperationMetric)
}

// ScannerPerformanceTracker tracks scanner-specific performance
type ScannerPerformanceTracker struct {
	logger            *PerformanceLogger
	scanTimes         map[string][]time.Duration // service -> scan times
	enrichTimes       map[string][]time.Duration // service -> enrich times
	mu                sync.RWMutex
	maxSamplesPerSvc int
}

// NewScannerPerformanceTracker creates a new scanner performance tracker
func NewScannerPerformanceTracker() *ScannerPerformanceTracker {
	return &ScannerPerformanceTracker{
		logger:            NewPerformanceLogger(true),
		scanTimes:         make(map[string][]time.Duration),
		enrichTimes:       make(map[string][]time.Duration),
		maxSamplesPerSvc: 100,
	}
}

// TrackScan tracks a scan operation
func (t *ScannerPerformanceTracker) TrackScan(service string, duration time.Duration, resourceCount int) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	// Record scan time
	if t.scanTimes[service] == nil {
		t.scanTimes[service] = make([]time.Duration, 0, t.maxSamplesPerSvc)
	}
	
	t.scanTimes[service] = append(t.scanTimes[service], duration)
	if len(t.scanTimes[service]) > t.maxSamplesPerSvc {
		t.scanTimes[service] = t.scanTimes[service][1:]
	}
	
	// Log if detailed logging is enabled
	if resourceCount > 0 {
		perResourceTime := duration / time.Duration(resourceCount)
		log.Printf("PERF: %s scan - %d resources in %v (avg %v/resource)", 
			service, resourceCount, duration, perResourceTime)
	}
}

// TrackEnrichment tracks an enrichment operation
func (t *ScannerPerformanceTracker) TrackEnrichment(service string, duration time.Duration) {
	t.mu.Lock()
	defer t.mu.Unlock()
	
	if t.enrichTimes[service] == nil {
		t.enrichTimes[service] = make([]time.Duration, 0, t.maxSamplesPerSvc)
	}
	
	t.enrichTimes[service] = append(t.enrichTimes[service], duration)
	if len(t.enrichTimes[service]) > t.maxSamplesPerSvc {
		t.enrichTimes[service] = t.enrichTimes[service][1:]
	}
}

// GetServiceStats returns performance statistics for a service
func (t *ScannerPerformanceTracker) GetServiceStats(service string) (scanStats, enrichStats *PerformanceStats) {
	t.mu.RLock()
	defer t.mu.RUnlock()
	
	if times, exists := t.scanTimes[service]; exists && len(times) > 0 {
		scanStats = calculateStats(times)
	}
	
	if times, exists := t.enrichTimes[service]; exists && len(times) > 0 {
		enrichStats = calculateStats(times)
	}
	
	return
}

// PerformanceStats contains statistical information
type PerformanceStats struct {
	Count   int
	Average time.Duration
	Min     time.Duration
	Max     time.Duration
	P50     time.Duration
	P90     time.Duration
	P99     time.Duration
}

// calculateStats calculates performance statistics
func calculateStats(times []time.Duration) *PerformanceStats {
	if len(times) == 0 {
		return nil
	}
	
	stats := &PerformanceStats{
		Count: len(times),
		Min:   times[0],
		Max:   times[0],
	}
	
	var total time.Duration
	for _, t := range times {
		total += t
		if t < stats.Min {
			stats.Min = t
		}
		if t > stats.Max {
			stats.Max = t
		}
	}
	
	stats.Average = total / time.Duration(len(times))
	
	// Calculate percentiles (simplified - in production use proper algorithm)
	stats.P50 = times[len(times)/2]
	stats.P90 = times[int(float64(len(times))*0.9)]
	if len(times) > 10 {
		stats.P99 = times[int(float64(len(times))*0.99)]
	} else {
		stats.P99 = stats.Max
	}
	
	return stats
}

// LogServiceSummary logs a summary for all services
func (t *ScannerPerformanceTracker) LogServiceSummary() {
	t.mu.RLock()
	services := make([]string, 0, len(t.scanTimes))
	for svc := range t.scanTimes {
		services = append(services, svc)
	}
	t.mu.RUnlock()
	
	log.Println("=== Scanner Performance Summary ===")
	
	for _, svc := range services {
		scanStats, enrichStats := t.GetServiceStats(svc)
		
		log.Printf("Service: %s", svc)
		
		if scanStats != nil {
			log.Printf("  Scan Performance (%d samples):", scanStats.Count)
			log.Printf("    Avg: %v, Min: %v, Max: %v", scanStats.Average, scanStats.Min, scanStats.Max)
			log.Printf("    P50: %v, P90: %v, P99: %v", scanStats.P50, scanStats.P90, scanStats.P99)
		}
		
		if enrichStats != nil {
			log.Printf("  Enrichment Performance (%d samples):", enrichStats.Count)
			log.Printf("    Avg: %v, Min: %v, Max: %v", enrichStats.Average, enrichStats.Min, enrichStats.Max)
			log.Printf("    P50: %v, P90: %v, P99: %v", enrichStats.P50, enrichStats.P90, enrichStats.P99)
		}
	}
}