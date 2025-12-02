# Nvidia Fan Control

A lightweight Linux utility for monitoring GPU temperatures and dynamically controlling NVIDIA GPU fan speeds using NVML.

## Requirements
- NVIDIA GPUs with NVML support
- NVIDIA drivers 520 or higher

## Build
```bash
go build -o nvidia-fan-control
```

## Configuration
```bash
vi config.json
```
```
{
	"time_to_update": 2,
	"temperature_ranges": [
		{ "min_temperature": -999, "max_temperature": 50, "fan_speed": 0, "hysteresis": 0 },
		{ "min_temperature": 50, "max_temperature": 60, "fan_speed": 40, "hysteresis": 8 },
		{ "min_temperature": 60, "max_temperature": 65, "fan_speed": 55, "hysteresis": 2 },
		{ "min_temperature": 65, "max_temperature": 70, "fan_speed": 70, "hysteresis": 2 },
		{ "min_temperature": 70, "max_temperature": 75, "fan_speed": 80, "hysteresis": 2 },
		{ "min_temperature": 75, "max_temperature": 80, "fan_speed": 85, "hysteresis": 2 },
		{ "min_temperature": 80, "max_temperature": 85, "fan_speed": 90, "hysteresis": 2 },
		{ "min_temperature": 85, "max_temperature": 95, "fan_speed": 95, "hysteresis": 2 },
		{ "min_temperature": 95, "max_temperature": 999, "fan_speed": 100, "hysteresis": 0 }
	]
}
```
## Hysteresis
Hysteresis is only applied when switching to a lower temperature range.
For instance, Stage 2 above is triggered as soon as the GPU is 50°C.
It will return to Stage 1 only when GPU is 42°C (min_temperature - hysteresis).

## Service
```bash
sudo vi /etc/systemd/system/nvidia-fan-control.service
```
Update `WorkingDirectory` to the directory containing `config.json`.
```
[Unit]
Description=NVIDIA Fan Control Service
After=sysinit.target

[Service]
ExecStart=/usr/bin/sudo /path/to/nvidia-fan-control
WorkingDirectory=/path/to/your/config
StandardOutput=file:/var/log/nvidia-fan-control.log
StandardError=inherit
Restart=always
User=root
Group=root

[Install]
WantedBy=multi-user.target
```

```bash
sudo systemctl daemon-reload
sudo systemctl enable nvidia-fan-control.service
sudo systemctl start nvidia-fan-control.service
sudo systemctl status nvidia-fan-control.service
```

### Check Logs
```bash
tail -f /var/log/nvidia-fan-control.log
```
