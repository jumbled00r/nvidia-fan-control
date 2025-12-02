package main

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"time"
	"github.com/NVIDIA/go-nvml/pkg/nvml"
)

type Config struct {
	TimeToUpdate float64 `json:"time_to_update"`
	TemperatureRanges []TemperatureRange `json:"temperature_ranges"`
}

type TemperatureRange struct {
	MinTemperature int `json:"min_temperature"`
	MaxTemperature int `json:"max_temperature"`
	FanSpeed int `json:"fan_speed"`
	Hysteresis int `json:"hysteresis"`
}

type DeviceMonitor struct {
	Index int
	Handle nvml.Device
	NumFans int
	CurrentFanSpeeds []int
	CurrentTemperatureRange TemperatureRange
}

func abs(x int) int {
	if x < 0 {
		return -x
	}
	return x
}

func getFanSpeedForTemperature(temp int, monitor *DeviceMonitor, ranges []TemperatureRange) int {
	currentSpeed := monitor.CurrentFanSpeeds[0]
	idealSpeed := currentSpeed
	var idealRange TemperatureRange
	for _, r := range ranges {
		if temp >= r.MinTemperature && temp <= r.MaxTemperature {
			idealSpeed = r.FanSpeed
			idealRange = r
		}
	}
	if idealSpeed > currentSpeed {
		monitor.CurrentTemperatureRange = idealRange
		return idealSpeed
	}
	if idealSpeed < currentSpeed {
		prevHighRange := monitor.CurrentTemperatureRange
		if prevHighRange.MaxTemperature == 0 {
			monitor.CurrentTemperatureRange = idealRange
			return idealSpeed
		}
		if temp <= prevHighRange.MinTemperature-prevHighRange.Hysteresis {
			monitor.CurrentTemperatureRange = idealRange
			return idealSpeed
		}
		return currentSpeed
	}
	if idealRange.MaxTemperature != 0 {
		monitor.CurrentTemperatureRange = idealRange
	}
	return currentSpeed
}

func setupLogging(logFilePath string) (*os.File, error) {
	logFile, err := os.OpenFile(logFilePath, os.O_TRUNC|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return nil, fmt.Errorf("failed to open log file %s: %w", logFilePath, err)
	}
	log.SetOutput(logFile)
	log.SetFlags(log.LstdFlags | log.Lshortfile)
	return logFile, nil
}

func loadConfig(file string) (Config, error) {
	var config Config
	data, err := os.ReadFile(file)
	if err != nil {
		return config, err
	}
	err = json.Unmarshal(data, &config)
	if config.TimeToUpdate <= 0 {
		log.Printf("WARN: time_to_update (%f) is invalid, defaulting to 2.0 seconds.", config.TimeToUpdate)
		config.TimeToUpdate = 2.0
	}
	log.Println("INFO: Configuration loaded.")
	return config, err
}

func initNVML() (func(), error) {
	if ret := nvml.Init(); ret != nvml.SUCCESS {
		return nil, fmt.Errorf("unable to initialize NVML: %v", nvml.ErrorString(ret))
	}
	return func() {
		if ret := nvml.Shutdown(); ret != nvml.SUCCESS {
			log.Printf("ERROR: Unable to shutdown NVML cleanly: %v", nvml.ErrorString(ret))
		}
	}, nil
}

func initDevices() ([]DeviceMonitor, error) {
	count, ret := nvml.DeviceGetCount()
	if ret != nvml.SUCCESS {
		return nil, fmt.Errorf("unable to get NVIDIA device count: %v", nvml.ErrorString(ret))
	}
	if count == 0 {
		return nil, fmt.Errorf("no NVIDIA devices found")
	}
	log.Printf("INFO: Found %d NVIDIA device(s).", count)
	monitors := []DeviceMonitor{}
	for i := 0; i < count; i++ {
		device, ret := nvml.DeviceGetHandleByIndex(i)
		if ret != nvml.SUCCESS {
			log.Printf("WARN: Unable to get handle for device %d: %v. Skipping.", i, nvml.ErrorString(ret))
			continue
		}
		numFans, ret := nvml.DeviceGetNumFans(device)
		if ret != nvml.SUCCESS || numFans <= 0 {
			log.Printf("INFO: Device %d reports 0 controllable fans or control not supported. Skipping.", i)
			continue
		}
		currentSpeeds := make([]int, numFans)
		temp, _ := nvml.DeviceGetTemperature(device, nvml.TEMPERATURE_GPU)
		for fanIdx := 0; fanIdx < numFans; fanIdx++ {
			speed, ret := nvml.DeviceGetFanSpeed_v2(device, fanIdx)
			if ret != nvml.SUCCESS {
				log.Printf("WARN: Failed to get initial speed for device %d Fan %d. Using 0.", i, fanIdx)
				speed = 0
			}
			currentSpeeds[fanIdx] = int(speed)
		}
		monitors = append(monitors, DeviceMonitor{
			Index: i,
			Handle: device,
			NumFans: numFans,
			CurrentFanSpeeds: currentSpeeds,
		})
		log.Printf("INFO: Initialized GPU %d: Temp=%d°C, FanSpeeds=%v%%", i, int(temp), currentSpeeds)
	}
	if len(monitors) == 0 && count > 0 {
		return nil, fmt.Errorf("found %d devices, but failed to initialize any for fan control", count)
	}
	return monitors, nil
}

func runMonitoringLoop(config Config, monitors []DeviceMonitor) {
	log.Println("INFO: Starting monitoring loop...")
	ticker := time.NewTicker(time.Duration(config.TimeToUpdate * float64(time.Second)))
	defer ticker.Stop()
	for range ticker.C {
		for i := range monitors {
			monitor := &monitors[i]
			temp, ret := nvml.DeviceGetTemperature(monitor.Handle, nvml.TEMPERATURE_GPU)
			if ret != nvml.SUCCESS {
				log.Printf("ERROR: Failed to get temperature for device %d: %v. Skipping cycle.", monitor.Index, nvml.ErrorString(ret))
				continue
			}
			tempInt := int(temp)
			newFanSpeed := getFanSpeedForTemperature(tempInt, monitor, config.TemperatureRanges)
			updatedFansIndices := []int{}
			for fanIdx := 0; fanIdx < monitor.NumFans; fanIdx++ {
				if newFanSpeed != monitor.CurrentFanSpeeds[fanIdx] {
					if ret := nvml.DeviceSetFanControlPolicy(monitor.Handle, fanIdx, nvml.FAN_POLICY_MANUAL); ret != nvml.SUCCESS && ret != nvml.ERROR_NOT_SUPPORTED {
						log.Printf("ERROR: Failed to set manual policy for GPU %d Fan %d: %v", monitor.Index, fanIdx, nvml.ErrorString(ret))
						continue
					}
					if ret := nvml.DeviceSetFanSpeed_v2(monitor.Handle, fanIdx, newFanSpeed); ret != nvml.SUCCESS {
						log.Printf("ERROR: Failed to set speed for GPU %d Fan %d to %d%%: %v", monitor.Index, fanIdx, newFanSpeed, nvml.ErrorString(ret))
						continue
					}
					monitor.CurrentFanSpeeds[fanIdx] = newFanSpeed
					updatedFansIndices = append(updatedFansIndices, fanIdx)
				}
			}
			if len(updatedFansIndices) > 0 {
				log.Printf("INFO: Updated GPU %d: Fans %v: Temp=%d°C, NewSpeeds=%v%%",
					monitor.Index, updatedFansIndices, tempInt, monitor.CurrentFanSpeeds)
			}
		}
	}
}

func main() {
	logFile, err := setupLogging("/var/log/nvidia-fan-control.log")
	if err != nil {
		log.Fatalf("FATAL: %v", err)
	}
	defer logFile.Close()
	config, err := loadConfig("config.json")
	if err != nil {
		log.Fatalf("FATAL: Failed to load config: %v", err)
	}
	nvmlCleanup, err := initNVML()
	if err != nil {
		log.Fatalf("FATAL: %v", err)
	}
	defer nvmlCleanup()
	monitors, err := initDevices()
	if err != nil {
		log.Fatalf("FATAL: %v", err)
	}
	if len(monitors) == 0 {
		log.Println("INFO: No devices with controllable fans were found or initialized. Exiting.")
		return
	}
	runMonitoringLoop(config, monitors)
	log.Println("INFO: Monitoring loop finished unexpectedly.")
}
