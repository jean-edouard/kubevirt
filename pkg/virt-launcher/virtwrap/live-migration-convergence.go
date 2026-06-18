/*
 * This file is part of the KubeVirt project
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 *
 * Copyright The KubeVirt Authors.
 *
 */

package virtwrap

import (
	"fmt"
	"time"

	"libvirt.org/go/libvirt"

	v1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/log"

	migrationutils "kubevirt.io/kubevirt/pkg/util/migrations"
	"kubevirt.io/kubevirt/pkg/virt-launcher/virtwrap/cli"
	"kubevirt.io/kubevirt/pkg/vmitrait"
)

const (
	stallMargin               float64 = 0.04
	stallProgressTimeout      int64   = 40
	switchoverTimeout         int64   = 60
	preCopyPossibleFactor     float64 = 1.5
	patienceWindowDecayFactor float64 = 0.5
	bandwidthEWMAAlpha        float64 = 0.4
	searchLocalMinima                 = true
)

type convergenceAction int

const (
	actionNothing convergenceAction = iota
	actionAbort
	actionPostCopy
	actionHardStopAndCopy
	actionSoftStopAndCopy
)

type iterationRecord struct {
	elapsedMs       uint64
	remainingBytes  uint64
	iterationNumber uint64
}

func (m *migrationMonitor) processCompletionTimeouts(dom cli.VirDomain, elapsedNs int64, estimatedDowntimeMs uint32, logger *log.FilteredLogger) {
	sd := m.stallDetector

	if !m.shouldTriggerTimeout(elapsedNs, logger) {
		return
	}

	if m.isMigrationPostCopy() {
		return
	}

	if sd.ewmaBandwidthBps == 0 {
		// In a typical migration, this case should not be possible.
		logger.Error("aborting migration due to illegal state: value of ewmaBandwidthBps not set!")
		m.l.cancelMigration(m.vmi)
		return
	}

	elapsedSeconds := elapsedNs / int64(time.Second)

	if !m.stallDetector.switchoverInitiated {

		// safety guard that protects against triggering a switch-over during a network drop
		completable := sd.canFinishByDeadline(elapsedSeconds, m.acceptableCompletionTime*2, estimatedDowntimeMs, logger)

		if m.options.AllowPostCopy && !vmitrait.HasVFIO(m.vmi) && completable {
			logger.Info("completion timeout reached: starting post-copy mode to force convergence")
			if err := dom.MigrateStartPostCopy(0); err != nil {
				logger.Reason(err).Error("failed to start post-copy migration")
				return
			}
			m.l.updateVMIMigrationMode(v1.MigrationPostCopy)
			sd.switchoverInitiated = true
			return
		}
		if m.options.AllowWorkloadDisruption && completable {
			logger.Infof("completion timeout reached: setting max downtime to %dms to force switchover", migrationutils.QEMUMaxMigrationDowntimeMS)
			if err := dom.MigrateSetMaxDowntime(migrationutils.QEMUMaxMigrationDowntimeMS, 0); err != nil {
				logger.Reason(err).Error("setting max downtime failed")
			}
			m.acceptableCompletionTime *= 2
			m.switchOverDeadline = elapsedSeconds + switchoverTimeout
			sd.switchoverInitiated = true
			return
		}

	}

	logger.Infof("aborting migration due to completion timeout: elapsedSec=%d acceptableCompletionSec=%d", elapsedSeconds, m.acceptableCompletionTime)
	m.l.cancelMigration(m.vmi)
}

func (m *migrationMonitor) triggerConvergenceAction(dom cli.VirDomain, action convergenceAction, reason string, logger *log.FilteredLogger) {
	sd := m.stallDetector

	sd.switchoverInitiated = true

	switch action {
	case actionNothing:
		sd.switchoverInitiated = false
		logger.V(3).Infof("convergence action is nothing because: %s", reason)
	case actionAbort:
		logger.Warningf("aborting migration: %s", reason)
		m.l.cancelMigration(m.vmi)
	case actionPostCopy:
		logger.Infof("starting post copy mode for migration: %s", reason)
		if err := dom.MigrateStartPostCopy(0); err != nil {
			sd.switchoverInitiated = false
			logger.Reason(err).Error("failed to start post migration")
			return
		}
		m.l.updateVMIMigrationMode(v1.MigrationPostCopy)
	case actionHardStopAndCopy, actionSoftStopAndCopy:
		now := time.Now().UTC().UnixNano()
		elapsedSeconds := (now - m.start) / int64(time.Second)

		// since stop-and-copy is not guaranteed to start immediately (or ever), a "switch-over" deadline is needed
		m.switchOverDeadline = elapsedSeconds + switchoverTimeout

		var downtime uint64
		if action == actionHardStopAndCopy {
			downtime = migrationutils.QEMUMaxMigrationDowntimeMS
			logger.Infof("forcing switchover by setting max downtime to %dms: %s", downtime, reason)
		} else {
			downtime = sd.maxDowntimeMs
			logger.Infof("max downtime set to %dms: %s", downtime, reason)
		}

		if err := dom.MigrateSetMaxDowntime(downtime, 0); err != nil {
			sd.switchoverInitiated = false
			logger.Reason(err).Error("setting max downtime failed")
		}

	default:
		logger.Error("unknown convergence action")
	}
}

// reconcile pause state (i.e. when QEMU triggers its internal switchover, update KubeVirt's state
// to reflect that the VM is now paused)
func (m *migrationMonitor) reconcilePauseState(dom cli.VirDomain, logger *log.FilteredLogger) {
	migrationState, stateReason, err := dom.GetState()
	if err != nil {
		logger.Reason(err).Error("failed to get migration state")
		return
	}
	logger.V(4).Infof("current migration state=%d and stateReason=%d", migrationState, stateReason)
	// The "!m.isMigrationPostCopy()" may seem redundant since in theory a post-copy VM should never report paused
	// reason as DOMAIN_PAUSED_MIGRATION. However, since QEMU itself does NOT make the DOMAIN_PAUSED_MIGRATION v.s.
	// DOMAIN_PAUSED_POSTCOPY distinction, LibVirt relies on internal state to determine which reason to use. This
	// internal state, however, can briefly be stale since LibVirt does not internally update it until QEMU itself
	// reports the VM has entered post-copy.
	if !m.isPausedMigration() && !m.isMigrationPostCopy() &&
		migrationState == libvirt.DOMAIN_PAUSED &&
		stateReason == int(libvirt.DOMAIN_PAUSED_MIGRATION) {
		logger.V(3).Infof("reconciling VM pause state")
		m.l.paused.add(m.vmi.UID)
		m.l.updateVMIMigrationMode(v1.MigrationPaused)
	}
}

func (m *migrationMonitor) decideAction(record iterationRecord, estimatedDowntimeMs uint32, logger *log.FilteredLogger) (convergenceAction, string) {

	sd := m.stallDetector

	if sd.switchoverInitiated {
		return actionNothing, "switchover already initiated"
	}

	if !sd.isAtLocalMinima(record, logger) && searchLocalMinima {
		return actionNothing, "not at a local minima yet"
	}

	var localMinLogMessage string
	if !searchLocalMinima {
		localMinLogMessage = "local minima search skipped: "
	} else {
		localMinLogMessage = "arrived at a local minima: "
	}
	logger.V(4).Infof(localMinLogMessage+"iterElapsedMs=%dms remainingBytes=%.2fMib bestRemainingBytes=%.2fMib impliedDowntimeMs=%dms maxDowntimeMs=%dms allowPostCopy=%t allowWorkloadDisruption=%t",
		m.iterationRecord.elapsedMs,
		bytesToMiB(record.remainingBytes),
		bytesToMiB(sd.bestRemainingBytes),
		estimatedDowntimeMs,
		sd.maxDowntimeMs,
		m.options.AllowPostCopy,
		m.options.AllowWorkloadDisruption,
	)

	now := time.Now().UTC().UnixNano()
	elapsedSeconds := (now - m.start) / int64(time.Second)

	// usually this case can only be triggered by a sudden network drop
	if !sd.canFinishByDeadline(elapsedSeconds, m.acceptableCompletionTime*2, estimatedDowntimeMs, logger) {
		return actionNothing, fmt.Sprintf("current estimated downtime (%dms) exceeds timeout budget by over two times", estimatedDowntimeMs)
	}

	if m.options.AllowWorkloadDisruption && m.options.AllowPostCopy && !vmitrait.HasVFIO(m.vmi) {
		return actionPostCopy, fmt.Sprintf("estimated downtime %dms is a local minima", estimatedDowntimeMs)
	}

	if m.options.AllowWorkloadDisruption {
		return actionHardStopAndCopy, fmt.Sprintf("estimated downtime %dms is a local minima", estimatedDowntimeMs)
	}

	if float64(estimatedDowntimeMs) <= float64(sd.maxDowntimeMs) {
		return actionSoftStopAndCopy, fmt.Sprintf("estimated downtime %dms within max allowed downtime %dms", estimatedDowntimeMs, sd.maxDowntimeMs)
	} else if float64(estimatedDowntimeMs) <= float64(sd.maxDowntimeMs)*preCopyPossibleFactor {
		return actionSoftStopAndCopy, fmt.Sprintf("estimated downtime %dms within tolerable factor %fx to max allowed downtime %dms", estimatedDowntimeMs, preCopyPossibleFactor, sd.maxDowntimeMs)
	}

	return actionAbort, fmt.Sprintf("estimated downtime %dms exceeds max allowed downtime %dms by a factor of more than x%.2f", estimatedDowntimeMs, sd.maxDowntimeMs, preCopyPossibleFactor)
}

func (m *migrationMonitor) handleStallDetection(dom cli.VirDomain, stats *libvirt.DomainJobInfo, elapsedNs int64, isIterationBoundary bool, logger *log.FilteredLogger) {

	// This stall detection mechanism implements VEP 248. In each iteration, pre-copy tries to transfer VM state data (i.e.
	// memory) from source to target. Multiple iterations are required because as the VM transfers data it is
	// actively dirtying new memory. For high-dirty rate VMs with a large writable working set, we would never
	// converge. Stall detection tracks how many bytes are left and if with in a progress timeout window we make
	// little to no progress we are stalled. Then the goal is to manually force trigger switch-over at a local minima
	// of remaining bytes. See VEP for more details.
	sd := m.stallDetector

	if !sd.initialMaxDowntimeSet {
		initialMaxDowntime := m.options.MaxDowntimeMs
		if initialMaxDowntime > migrationutils.QEMUDefaultTargetDowntimeMS {
			initialMaxDowntime = migrationutils.QEMUDefaultTargetDowntimeMS
		}
		if err := dom.MigrateSetMaxDowntime(initialMaxDowntime, 0); err != nil {
			logger.Reason(err).Warning("failed to set initial max downtime")
		}
		sd.initialMaxDowntimeSet = true
	}

	m.reconcilePauseState(dom, logger)

	if !m.isAbortInProgress() {
		if stats != nil && stats.Type == libvirt.DOMAIN_JOB_UNBOUNDED &&
			stats.DataRemainingSet && stats.TimeElapsedSet && stats.MemIterationSet {
			// the value in m.iterationRecord is accurate only when (1) we are the start an iteration or (2) if the
			//  VM is paused or (3) if the VM is in post-copy.
			if isIterationBoundary {
				logger.V(4).Info("processing migration iteration boundary for stall detection")
				m.iterationRecord.remainingBytes = stats.DataRemaining
				m.iterationRecord.elapsedMs = stats.TimeElapsed
				m.iterationRecord.iterationNumber = stats.MemIteration
				if stalled := sd.processStallDetectionIteration(m.iterationRecord, logger); stalled {
					estimatedDowntimeMs := sd.estimateDowntimeMs(m.iterationRecord, logger)
					action, reason := m.decideAction(m.iterationRecord, estimatedDowntimeMs, logger)
					m.triggerConvergenceAction(dom, action, reason, logger)
				}
			} else if m.isPausedMigration() || m.isMigrationPostCopy() {
				m.iterationRecord.remainingBytes = stats.DataRemaining
				m.iterationRecord.elapsedMs = stats.TimeElapsed
				m.iterationRecord.iterationNumber = stats.MemIteration
			} else if stats.MemBpsSet {
				sd.updateBandwidthEstimate(stats.MemBps, logger)
			}
		} else if stats == nil {
			logger.V(3).Info("skipping actions for stall detection due to missing job stats")
		} else {
			logger.V(3).Infof("skipping actions for stall detection due to missing stats data: DataRemainingSet=%t, TimeElapsedSet=%t, MemBpsSet=%t", stats.DataRemainingSet, stats.TimeElapsedSet, stats.MemBpsSet)
		}

		estimatedDowntimeMs := sd.estimateDowntimeMs(m.iterationRecord, logger)
		m.processCompletionTimeouts(dom, elapsedNs, estimatedDowntimeMs, logger)
	}
}

func (m *migrationMonitor) registerIterationCallback(domName string) (int, error) {
	return m.l.virConn.DomainEventMigrationIterationRegister(func(_ *libvirt.Connect, domain *libvirt.Domain, event *libvirt.DomainEventMigrationIteration) {
		name, err := domain.GetName()
		if err != nil || name != domName {
			return
		}

		select {
		case m.iterationCh <- event.Iteration:
			m.logger.V(4).Infof("queued migration iteration event for iteration #%d", event.Iteration)
		default:
			m.logger.V(3).Infof("dropped migration iteration event for iteration #%d: reason=channel-full", event.Iteration)
		}
	})
}
