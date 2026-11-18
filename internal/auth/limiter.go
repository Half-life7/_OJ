package auth

import "time"

type attempt struct {
	failures []time.Time
	inflight int
	touched  time.Time
}

// Reserving a slot before hashing prevents simultaneous login attempts from
// slipping past the five-failure limit. State is bounded and expires lazily.
func (s *Service) beginAttempt(key string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.clock()
	cutoff := now.Add(-10 * time.Minute)
	if len(s.attempts) >= 10000 {
		for k, a := range s.attempts {
			if a.inflight == 0 && !a.touched.After(cutoff) {
				delete(s.attempts, k)
			}
		}
	}
	a := s.attempts[key]
	if a == nil {
		if len(s.attempts) >= 10000 {
			return false
		}
		a = &attempt{}
		s.attempts[key] = a
	}
	for len(a.failures) > 0 && !a.failures[0].After(cutoff) {
		a.failures = a.failures[1:]
	}
	if len(a.failures)+a.inflight >= 5 {
		return false
	}
	a.inflight++
	a.touched = now
	return true
}
func (s *Service) finishAttempt(key string, failed bool) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := s.attempts[key]
	if a == nil {
		return false
	}
	a.inflight--
	a.touched = s.clock()
	if failed {
		a.failures = append(a.failures, a.touched)
	} else {
		a.failures = nil
	}
	return len(a.failures) >= 5
}
