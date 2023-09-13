package heartbeat

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"strconv"
	"strings"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	kubevirtv1 "kubevirt.io/api/core/v1"
	"kubevirt.io/client-go/log"

	virtconfig "kubevirt.io/kubevirt/pkg/virt-config"
)

const (
	// In some environments, sysfs is mounted read-only even for privileged
	// containers: https://github.com/containerd/containerd/issues/8445.
	// Use the path from the host filesystem.
	ksmBasePath  = "/proc/1/root/sys/kernel/mm/ksm/"
	ksmRunPath   = ksmBasePath + "run"
	ksmSleepPath = ksmBasePath + "sleep_millisecs"
	ksmPagesPath = ksmBasePath + "pages_to_scan"

	memInfoPath = "/proc/meminfo"

	ksmPagesBoost      = 300
	ksmPagesDecay      = -50
	ksmNpagesMin       = 64
	ksmNpagesMax       = 1250
	ksmNpagesDefault   = 100
	ksmSleepMsBaseline = 10
	ksmFreePercent     = 0.2
)

// Inspired from https://github.com/artyom/meminfo
func getTotalAndAvailableMem() (uint64, uint64, error) {
	var total, available uint64
	f, err := os.Open(memInfoPath)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	s := bufio.NewScanner(f)
	found := 0
	for s.Scan() && found < 2 {
		switch {
		case bytes.HasPrefix(s.Bytes(), []byte(`MemTotal:`)):
			_, err = fmt.Sscanf(s.Text(), "MemTotal:%d", &total)
			found++
		case bytes.HasPrefix(s.Bytes(), []byte(`MemAvailable:`)):
			_, err = fmt.Sscanf(s.Text(), "MemAvailable:%d", &available)
			found++
		default:
			continue
		}
		if err != nil {
			return 0, 0, err
		}
	}
	if found != 2 {
		return 0, 0, fmt.Errorf("failed to find total and available memory")
	}

	return total, available, nil
}

func getKsmPages() (int, error) {
	pagesBytes, err := os.ReadFile(ksmPagesPath)
	if err != nil {
		return 0, err
	}

	pages, err := strconv.Atoi(strings.TrimSpace(string(pagesBytes)))
	if err != nil {
		return 0, err
	}

	return pages, nil
}

// Inspired from https://github.com/oVirt/mom/blob/master/doc/ksm.rules
func calculateNewRunSleepAndPages(running bool) (bool, uint64, int, error) {
	total, available, err := getTotalAndAvailableMem()
	if err != nil {
		return false, 0, 0, err
	}
	pages, err := getKsmPages()
	if err != nil {
		return false, 0, 0, err
	}

	sleep := ksmSleepMsBaseline * (16 * 1024 * 1024) / (total - available)

	if float32(available) > float32(total)*ksmFreePercent {
		// No memory pressure. Reduce or stop KSM activity
		if running {
			pages += ksmPagesDecay
			if pages < ksmNpagesMin {
				pages = ksmNpagesMin
				running = false
			}
			return running, sleep, pages, nil
		} else {
			return false, 0, 0, nil
		}
	} else {
		// We're under memory pressure and KSM is already running. Increase or start KSM activity
		if running {
			pages += ksmPagesBoost
			if pages > ksmNpagesMax {
				pages = ksmNpagesMax
			}
			return true, sleep, pages, nil
		} else {
			return true, sleep, ksmNpagesDefault, nil
		}
	}
}

func writeKsmValuesToFiles(running bool, sleep uint64, pages int) error {
	run := "0"
	if running {
		run = "1"

		err := os.WriteFile(ksmSleepPath, []byte(strconv.FormatUint(sleep, 10)), 0644)
		if err != nil {
			return err
		}
		err = os.WriteFile(ksmPagesPath, []byte(strconv.Itoa(pages)), 0644)
		if err != nil {
			return err
		}
	}
	err := os.WriteFile(ksmRunPath, []byte(run), 0644)
	if err != nil {
		return err
	}

	return nil
}

func loadKSM() (bool, bool) {
	ksmValue, err := os.ReadFile(ksmRunPath)
	if err != nil {
		log.DefaultLogger().Warningf("An error occurred while reading the ksm module file; Maybe it is not available: %s", err)
		// Only enable for ksm-available nodes
		return false, false
	}

	return true, bytes.Equal(ksmValue, []byte("1\n"))
}

// handleKSM will update the ksm of the node (if available) based on the kv configuration and
// will set the outcome value to the n.KSM struct
// If the node labels match the selector terms, the ksm will be enabled.
// Empty Selector will enable ksm for every node
func handleKSM(node *v1.Node, clusterConfig *virtconfig.ClusterConfig) (bool, bool) {
	available, running := loadKSM()
	if !available {
		return running, false
	}

	ksmConfig := clusterConfig.GetKSMConfiguration()
	if ksmConfig == nil {
		if disableKSM(node, running) {
			return false, false
		} else {
			return running, false
		}
	}

	selector, err := metav1.LabelSelectorAsSelector(ksmConfig.NodeLabelSelector)
	if err != nil {
		log.DefaultLogger().Errorf("An error occurred while converting the ksm selector: %s", err)
		return running, false
	}

	if !selector.Matches(labels.Set(node.ObjectMeta.Labels)) {
		if disableKSM(node, running) {
			return false, false
		} else {
			return running, false
		}
	}

	newRunning, sleep, pages, err := calculateNewRunSleepAndPages(running)
	if err != nil {
		log.DefaultLogger().Reason(err).Errorf("An error occurred while calculating the new KSM values")
		return running, false
	}
	err = writeKsmValuesToFiles(newRunning, sleep, pages)
	if err != nil {
		log.DefaultLogger().Reason(err).Errorf("An error occurred while writing the new KSM values")
		return running, false
	}

	return newRunning, newRunning
}

func disableKSM(node *v1.Node, enabled bool) bool {
	if enabled {
		if _, found := node.GetAnnotations()[kubevirtv1.KSMHandlerManagedAnnotation]; found {
			err := os.WriteFile(ksmRunPath, []byte("0\n"), 0644)
			if err != nil {
				log.DefaultLogger().Errorf("Unable to write ksm: %s", err.Error())
				return false
			}
			return true
		}
	}
	return false
}
