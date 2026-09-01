package cliwrappers

import (
	"math/rand/v2"
	"regexp"
	"slices"
	"time"

	l "github.com/konflux-ci/konflux-build-cli/pkg/logger"
)

var retryerLog = l.Logger.WithField("logger", "Retryer")

// Backdoor for tests
var DisableRetryer bool = false

// Retryer runs given command until it succeeds or a stop condition is met.
// After the first failure, it waits BaseDelay before next attempt.
// After each next failure, the dalay is multiplied by DelayFactor,
// but cannot be greather than MaxDelay if MaxDelay is positive.
// When JitterFraction is positive, each wait is randomized within
// [delay*(1-JitterFraction), delay] to avoid synchronized retries.
// Stop conditions:
// - MaxAttempts is reached
// - The command exited with a stop exit code
// - The command output (stdout or stderr) contained a stop substring or matched a stop regexp.
type Retryer struct {
	BaseDelay      time.Duration
	DelayFactor    float64
	MaxAttempts    int
	MaxDelay       time.Duration
	JitterFraction float64

	cliCall func() (stdout string, stderr string, errCode int, err error)

	stopExitCodes   []int
	stopErrorRegexs []*regexp.Regexp
}

func NewRetryer(cliCall func() (stdout string, stderr string, errCode int, err error)) *Retryer {
	return &Retryer{
		BaseDelay:   1 * time.Second,
		DelayFactor: 2,
		MaxAttempts: 3,

		cliCall: cliCall,
	}
}

// Run executes the provided via constructor command with specified retries strategy.
// Returns stdout, stderr, errCode, error of the last run.
func (r *Retryer) Run() (stdout string, stderr string, errCode int, err error) {
	if DisableRetryer {
		return r.cliCall()
	}

	retryerLog.Debugf("Running with max retries %d, %v interval, %.2f interval factor", r.MaxAttempts, r.BaseDelay, r.DelayFactor)

	delay := r.BaseDelay
	for attempt := 1; attempt <= r.MaxAttempts; attempt++ {
		stdout, stderr, errCode, err = r.cliCall()
		if err == nil {
			return //nolint:nilerr
		}

		if slices.Contains(r.stopExitCodes, errCode) {
			retryerLog.Debugf("Stopping retries after attempt %d, because cli exited with return code: %d", attempt, errCode)
			return
		}
		for _, stopRegex := range r.stopErrorRegexs {
			if stopRegex.MatchString(stdout) || stopRegex.MatchString(stderr) {
				retryerLog.Debugf("Stopping retries after attempt %d, because cli output matched stop regex: %s", attempt, stopRegex.String())
				return
			}
		}

		if attempt == r.MaxAttempts {
			// It was the last iteration, no need to wait after it.
			retryerLog.Debugf("Attempt %d failed, output:\n[stdout]:\n%s\n[stderr]:\n%s", attempt, stdout, stderr)
			break
		}

		sleepFor := r.applyJitter(delay)
		retryerLog.Debugf("Attempt %d failed, output:\n[stdout]:\n%s\n[stderr]:\n%s\nWaiting %v before next retry", attempt, stdout, stderr, sleepFor)
		time.Sleep(sleepFor)
		delay = time.Duration(float64(delay) * r.DelayFactor)
		if r.MaxDelay > 0 && delay > r.MaxDelay {
			delay = r.MaxDelay
		}
	}

	retryerLog.Infof("Giving up on command after %d attempts", r.MaxAttempts)
	return
}

// WithBaseDelay sets the initial delay after a failure.
// The delay will be increased by DelayFactor times after each failure.
func (r *Retryer) WithBaseDelay(baseInterval time.Duration) *Retryer {
	r.BaseDelay = baseInterval
	return r
}

// WithDelayFactor sets the delay increasing factor after a failure.
func (r *Retryer) WithDelayFactor(delayFactor float64) *Retryer {
	r.DelayFactor = delayFactor
	return r
}

// WithConstantDelay makes all dalays after failures of the same duration.
func (r *Retryer) WithConstantDelay(delay time.Duration) *Retryer {
	r.BaseDelay = delay
	r.DelayFactor = 1
	return r
}

// WithMaxAttempts sets maximum number or attempts before give up and fail.
func (r *Retryer) WithMaxAttempts(maxAttempts int) *Retryer {
	r.MaxAttempts = maxAttempts
	return r
}

// WithMaxDelay sets maximum delay to wait between attempts.
// If not set, no limit is appled.
func (r *Retryer) WithMaxDelay(maxDelay time.Duration) *Retryer {
	r.MaxDelay = maxDelay
	return r
}

// WithJitter sets the fraction of each retry delay that is randomized.
// The wait becomes uniform in [delay*(1-fraction), delay].
// fraction is clamped to [0, 1]; 0 disables jitter.
func (r *Retryer) WithJitter(fraction float64) *Retryer {
	if fraction < 0 {
		fraction = 0
	}
	if fraction > 1 {
		fraction = 1
	}
	r.JitterFraction = fraction
	return r
}

// applyJitter returns sleep duration with optional jitter applied to delay.
func (r *Retryer) applyJitter(delay time.Duration) time.Duration {
	if delay <= 0 || r.JitterFraction <= 0 {
		return delay
	}
	minDelay := time.Duration(float64(delay) * (1 - r.JitterFraction))
	span := delay - minDelay
	if span <= 0 {
		return delay
	}
	//nolint:gosec // G404: math/rand is intentional for retry jitter, not cryptographic use
	return minDelay + rand.N(span+1)
}

// StopOnExitCode adds an stop exit code.
// If command exits with such exit code, no more retry attempts performed.
func (r *Retryer) StopOnExitCode(exitCode int) *Retryer {
	r.stopExitCodes = append(r.stopExitCodes, exitCode)
	return r
}

// StopOnExitCodes adds an stop exit codes.
// If command exits with such exit code, no more retry attempts performed.
func (r *Retryer) StopOnExitCodes(exitCodes ...int) *Retryer {
	r.stopExitCodes = append(r.stopExitCodes, exitCodes...)
	return r
}

// StopIfOutputMatches adds a stop regex.
// If command output (stdout or stderr) matches the regex, no more retry attempts performed.
func (r *Retryer) StopIfOutputMatches(regexString string) *Retryer {
	// The given regex is not expected to be user defined.
	// Fail fast if the regex is invalid.
	stopRegex := regexp.MustCompile(regexString)
	r.stopErrorRegexs = append(r.stopErrorRegexs, stopRegex)
	return r
}

// StopIfOutputContains adds a stop string.
// If command output (stdout or stderr) contains the given string, no more retry attempts performed.
func (r *Retryer) StopIfOutputContains(stopString string) *Retryer {
	return r.StopIfOutputMatches("(?i)" + regexp.QuoteMeta(stopString))
}

// WithImageRegistryPreset sets retryer parameters for interacting with an image registry scenario.
func (r *Retryer) WithImageRegistryPreset() *Retryer {
	r.BaseDelay = 1 * time.Second
	r.DelayFactor = 2
	r.MaxAttempts = 10
	r.MaxDelay = 4 * time.Minute
	r.JitterFraction = 0.5
	return r
}
