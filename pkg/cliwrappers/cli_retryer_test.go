package cliwrappers_test

import (
	"errors"
	"testing"
	"time"

	. "github.com/onsi/gomega"

	"github.com/konflux-ci/konflux-build-cli/pkg/cliwrappers"
)

func TestNewRetryer(t *testing.T) {
	g := NewWithT(t)

	t.Run("should create new Retryer instance", func(t *testing.T) {
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			return "", "", 0, nil
		})

		g.Expect(retryer).ToNot(BeNil())
		g.Expect(retryer.BaseDelay).To(BeNumerically(">", 0))
		g.Expect(retryer.DelayFactor).To(BeNumerically(">", 0))
		g.Expect(retryer.MaxAttempts).To(BeNumerically(">", 0))
	})
}

func TestRetryer_Config(t *testing.T) {
	g := NewWithT(t)

	cliFunc := func() (string, string, int, error) {
		return "", "", 0, nil
	}

	t.Run("should be able to set retry params", func(t *testing.T) {
		const baseDelay = 7 * time.Second
		const delayFactor float64 = 2.5
		const maxAttempts = 8
		const maxDelay = 100 * time.Second

		retryer := cliwrappers.NewRetryer(cliFunc)

		retryer.
			WithBaseDelay(baseDelay).
			WithDelayFactor(delayFactor).
			WithMaxAttempts(maxAttempts).
			WithMaxDelay(maxDelay)

		g.Expect(retryer.BaseDelay).To(Equal(baseDelay))
		g.Expect(retryer.DelayFactor).To(Equal(delayFactor))
		g.Expect(retryer.MaxAttempts).To(Equal(maxAttempts))
		g.Expect(retryer.MaxDelay).To(Equal(maxDelay))
	})

	t.Run("should be able to set constant interval", func(t *testing.T) {
		retryer := cliwrappers.NewRetryer(cliFunc)

		const constInterval = 15 * time.Second

		retryer.WithConstantDelay(constInterval)

		g.Expect(retryer.BaseDelay).To(Equal(constInterval))
		g.Expect(retryer.DelayFactor).To(Equal(1.0))
		g.Expect(retryer.MaxAttempts).To(BeNumerically(">", 0))
	})

	t.Run("should be able to set and clamp jitter fraction", func(t *testing.T) {
		retryer := cliwrappers.NewRetryer(cliFunc)

		retryer.WithJitter(0.5)
		g.Expect(retryer.JitterFraction).To(Equal(0.5))

		retryer.WithJitter(-1)
		g.Expect(retryer.JitterFraction).To(Equal(0.0))

		retryer.WithJitter(2)
		g.Expect(retryer.JitterFraction).To(Equal(1.0))
	})

	t.Run("image registry preset enables jitter", func(t *testing.T) {
		retryer := cliwrappers.NewRetryer(cliFunc).WithImageRegistryPreset()
		g.Expect(retryer.JitterFraction).To(Equal(0.5))
		g.Expect(retryer.MaxAttempts).To(Equal(10))
	})

	t.Run("should be able to use configuration presets", func(t *testing.T) {
		configurationPresets := []func(*cliwrappers.Retryer) *cliwrappers.Retryer{
			(*cliwrappers.Retryer).WithImageRegistryPreset,
		}

		for _, preset := range configurationPresets {
			retryer := cliwrappers.NewRetryer(cliFunc)
			oldRetryer := *retryer

			preset(retryer)

			g.Expect(retryer.BaseDelay).To(BeNumerically(">", 0))
			g.Expect(retryer.DelayFactor).To(BeNumerically(">", 0.0))
			g.Expect(retryer.MaxAttempts).To(BeNumerically(">", 0))

			g.Expect(&oldRetryer).ToNot(Equal(retryer))
		}
	})
}

func TestRetryer_Run(t *testing.T) {
	g := NewWithT(t)

	t.Run("should be able to retry command execution", func(t *testing.T) {
		const failTimes = 6

		attempt := 0
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			attempt++
			exitCode := 1
			err := errors.New("command has failed")
			if attempt > failTimes {
				exitCode = 0
				err = nil
			}
			return "stdout", "stderr", exitCode, err
		}).WithConstantDelay(1 * time.Millisecond).WithMaxAttempts(failTimes + 4)

		stdout, stderr, exitCode, err := retryer.Run()

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(exitCode).To(Equal(0))
		g.Expect(stdout).To(Equal("stdout"))
		g.Expect(stderr).To(Equal("stderr"))
		g.Expect(attempt).To(Equal(failTimes + 1))
	})

	t.Run("should fail command execution if max attempts reached", func(t *testing.T) {
		const maxAttempts = 5

		attempt := 0
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			attempt++
			return "", "", 10, errors.New("command has failed")
		}).WithConstantDelay(1 * time.Millisecond).WithMaxAttempts(maxAttempts)

		_, _, exitCode, err := retryer.Run()

		g.Expect(err).To(HaveOccurred())
		g.Expect(exitCode).ToNot(Equal(0))
		g.Expect(attempt).To(Equal(maxAttempts))
	})

	t.Run("should increase delay duration on failures", func(t *testing.T) {
		const succeedAtAttempt = 4

		attempt := 0
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			attempt++
			if attempt == succeedAtAttempt {
				return "", "", 0, nil
			}
			return "", "", 1, errors.New("command has failed")
		}).
			WithMaxAttempts(10).
			// According to the retry settings below, the delays should be:
			// 0 + 4 + 20 + 100 = 124 ms
			WithBaseDelay(4 * time.Millisecond).
			WithDelayFactor(5)

		start := time.Now()
		_, _, _, err := retryer.Run()
		elapsed := time.Since(start)

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(attempt).To(Equal(succeedAtAttempt))
		g.Expect(elapsed).To(BeNumerically(">", 120*time.Millisecond))
		g.Expect(elapsed).To(BeNumerically("<", 200*time.Millisecond))
	})

	t.Run("should limit max delay on failures", func(t *testing.T) {
		const succeedAtAttempt = 10

		attempt := 0
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			attempt++
			if attempt == succeedAtAttempt {
				return "", "", 0, nil
			}
			return "", "", 1, errors.New("command has failed")
		}).
			WithMaxAttempts(10).
			// According to the retry settings below, the delays should be:
			// 0 + 1 + 2 + 4 + 8 + 10 * 5 = 65 ms
			WithBaseDelay(1 * time.Millisecond).
			WithDelayFactor(2).
			WithMaxDelay(10 * time.Millisecond)

		start := time.Now()
		_, _, _, err := retryer.Run()
		elapsed := time.Since(start)

		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(attempt).To(Equal(succeedAtAttempt))
		g.Expect(elapsed).To(BeNumerically(">", 60*time.Millisecond))
		g.Expect(elapsed).To(BeNumerically("<", 100*time.Millisecond))
	})

	t.Run("should not wait after last failure before stop", func(t *testing.T) {
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			return "", "", 1, errors.New("command has failed")
		}).
			WithMaxAttempts(3).
			// According to the retry settings below, the delays should be:
			// 0 + 1 + 25 = 26 ms.
			WithBaseDelay(1 * time.Millisecond).
			WithDelayFactor(25)

		start := time.Now()
		_, _, _, err := retryer.Run()
		elapsed := time.Since(start)

		g.Expect(err).To(HaveOccurred())
		g.Expect(elapsed).To(BeNumerically(">", 25*time.Millisecond))
		g.Expect(elapsed).To(BeNumerically("<", 50*time.Millisecond))
	})

	t.Run("should be able to stop retries on command exit code", func(t *testing.T) {
		const stopExitCode = 123
		const changeExitCodeAtATtempt = 4

		attempt := 0
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			attempt++
			exitCode := 1
			if attempt == changeExitCodeAtATtempt {
				exitCode = stopExitCode
			}
			return "", "", exitCode, errors.New("command has failed")
		}).WithConstantDelay(1*time.Millisecond).WithMaxAttempts(changeExitCodeAtATtempt+5).
			StopOnExitCode(10).StopOnExitCode(stopExitCode).StopOnExitCodes(12, 15)

		_, _, exitCode, err := retryer.Run()

		g.Expect(err).To(HaveOccurred())
		g.Expect(exitCode).To(Equal(stopExitCode))
		g.Expect(attempt).To(Equal(changeExitCodeAtATtempt))
	})

	t.Run("should be able to stop retries on regex match in stdout", func(t *testing.T) {
		const stopRegexPattern = `Error\s+[45]\d\d`
		const stopStdout = "Starting...\nProcessing...\nError 523 happened\nFailed"
		const returnStopRegexMatchAtAttempt = 3

		attempt := 0
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			attempt++
			stdout := "Starting...\nProcessing...\nError 345 happened\nFailed"
			if attempt == returnStopRegexMatchAtAttempt {
				stdout = stopStdout
			}
			return stdout, "failure", 1, errors.New("command has failed")
		}).WithConstantDelay(1 * time.Millisecond).WithMaxAttempts(returnStopRegexMatchAtAttempt + 2).
			StopIfOutputMatches(stopRegexPattern)

		stdout, _, _, err := retryer.Run()

		g.Expect(err).To(HaveOccurred())
		g.Expect(stdout).To(Equal(stopStdout))
		g.Expect(attempt).To(Equal(returnStopRegexMatchAtAttempt))
	})

	t.Run("should be able to stop retries on regex match in stderr", func(t *testing.T) {
		const stopRegexPattern = `Error\s+[45]\d\d`
		const stopStderr = "Starting...\nProcessing...\nError 432 happened\nFailed"
		const returnStopRegexMatchAtAttempt = 2

		attempt := 0
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			attempt++
			stderr := "Starting...\nProcessing...\nError 345 happened\nFailed"
			if attempt == returnStopRegexMatchAtAttempt {
				stderr = stopStderr
			}
			return "working on request", stderr, 1, errors.New("command has failed")
		}).WithConstantDelay(1 * time.Millisecond).WithMaxAttempts(returnStopRegexMatchAtAttempt + 3).
			StopIfOutputMatches(stopRegexPattern)

		_, stderr, _, err := retryer.Run()

		g.Expect(err).To(HaveOccurred())
		g.Expect(stderr).To(Equal(stopStderr))
		g.Expect(attempt).To(Equal(returnStopRegexMatchAtAttempt))
	})

	t.Run("should be able to stop retries on string match in stdout", func(t *testing.T) {
		const stopString = `unauthorized`
		const stopStdout = "Sending request...\n401 Unauthorized\nFailed"
		const returnStopStringAtAttempt = 1

		attempt := 0
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			attempt++
			stdout := "Sending request...\n503 Service Unavailable\nFailed"
			if attempt == returnStopStringAtAttempt {
				stdout = stopStdout
			}
			return stdout, "failure", 1, errors.New("command has failed")
		}).WithConstantDelay(1 * time.Millisecond).WithMaxAttempts(returnStopStringAtAttempt + 5).
			StopIfOutputContains(stopString)

		stdout, _, _, err := retryer.Run()

		g.Expect(err).To(HaveOccurred())
		g.Expect(stdout).To(Equal(stopStdout))
		g.Expect(attempt).To(Equal(returnStopStringAtAttempt))
	})

	t.Run("should be able to stop retries on string match in stderr", func(t *testing.T) {
		const stopString = `forbidden`
		const stopStderr = "Sending request...\n403 Forbidden\nFailed"
		const returnStopStringAtAttempt = 5

		attempt := 0
		retryer := cliwrappers.NewRetryer(func() (string, string, int, error) {
			attempt++
			stderr := "Sending request...\n503 Service Unavailable\nFailed"
			if attempt == returnStopStringAtAttempt {
				stderr = stopStderr
			}
			return "connecting", stderr, 1, errors.New("command has failed")
		}).WithConstantDelay(1 * time.Millisecond).WithMaxAttempts(returnStopStringAtAttempt + 2).
			StopIfOutputContains(stopString)

		_, stderr, _, err := retryer.Run()

		g.Expect(err).To(HaveOccurred())
		g.Expect(stderr).To(Equal(stopStderr))
		g.Expect(attempt).To(Equal(returnStopStringAtAttempt))
	})
}
