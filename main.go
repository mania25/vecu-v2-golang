package main

import (
	"context"
	"encoding/hex"
	"fmt"
	"log"
	"math/rand"
	"sync"
	"time"

	"go.einride.tech/can"
	"go.einride.tech/can/pkg/socketcan"
)

// CANMessage represents each command in the DBC database.
type CANMessage struct {
	ID      uint32
	Name    string
	DataLen uint8
	Decode  func(data []byte) string
}

// Define the DBC-like structure with commands and required data length.
var CAN_DBC = map[uint32]CANMessage{
	0x100: {ID: 0x100, Name: "EngineOnOff", DataLen: 8, Decode: decodeEngineOnOff},
	0x101: {ID: 0x101, Name: "FrontLight", DataLen: 8, Decode: decodeFrontLight},
	0x200: {ID: 0x200, Name: "EngineTempSensor", DataLen: 8, Decode: decodeEngineTemp},
	0x201: {ID: 0x201, Name: "InjectorTimingSensor", DataLen: 8, Decode: decodeInjectorTiming},
	0x202: {ID: 0x202, Name: "OxygenSensor", DataLen: 8, Decode: decodeOxygenSensor},
	0x203: {ID: 0x203, Name: "FuelTankLevel", DataLen: 8, Decode: decodeFuelTankLevel},
	0x204: {ID: 0x204, Name: "ThrottlePosition", DataLen: 8, Decode: decodeThrottlePosition},
	0x205: {ID: 0x205, Name: "EngineRPM", DataLen: 8, Decode: decodeEngineRPM},
}

// Global variables to track engine state and control simulation.
var (
	engineOn      bool
	simulationMux sync.Mutex
)

func init() {
	rand.Seed(time.Now().UnixNano())
}

// Helper functions to generate fluctuating sensor values within specific ranges.
func fluctuate(min, max int) int {
	return min + rand.Intn(max-min+1)
}

// Decoding functions for each command.
func decodeEngineOnOff(data []byte) string {
	if data[0] == 1 {
		return "Engine ON"
	}
	return "Engine OFF"
}

func decodeFrontLight(data []byte) string {
	if data[0] == 1 {
		return "Front Light ON"
	}
	return "Front Light OFF"
}

func decodeEngineTemp(data []byte) string {
	temp := int(data[0])<<8 | int(data[1])
	return fmt.Sprintf("Engine Temperature: %d °C", temp)
}

func decodeInjectorTiming(data []byte) string {
	timing := int(data[0])<<8 | int(data[1])
	return fmt.Sprintf("Injector Timing: %d ms", timing)
}

func decodeOxygenSensor(data []byte) string {
	return fmt.Sprintf("Oxygen Sensor: %d%%", data[0])
}

func decodeFuelTankLevel(data []byte) string {
	return fmt.Sprintf("Fuel Tank Level: %d%%", data[0])
}

func decodeThrottlePosition(data []byte) string {
	return fmt.Sprintf("Throttle Position: %d%%", data[0])
}

func decodeEngineRPM(data []byte) string {
	rpm := int(data[0])<<8 | int(data[1])
	return fmt.Sprintf("Engine RPM: %d", rpm)
}

// simulateSensors continuously sends fluctuating sensor data to the CAN bus if the engine is on.
func simulateSensors(ctx context.Context) {
	log.Println("Opening TX CAN interface. . .")

	conn, err := socketcan.DialContext(ctx, "can", "vcan0")
	if err != nil {
		log.Fatalf("failed to connect to vcan0 for sensor simulation: %v", err)
	}
	defer conn.Close()

	log.Println("Prepare for transmitting message through TX CAN interface. . .")
	tx := socketcan.NewTransmitter(conn)

	for {
		simulationMux.Lock()
		if !engineOn {
			simulationMux.Unlock()
			return
		}
		simulationMux.Unlock()

		// Generate fluctuating sensor values within defined ranges
		engineTemp := fluctuate(80, 100)      // Engine Temp: 80 - 100 °C
		injectorTiming := fluctuate(60, 90)   // Injector Timing: 60 - 90 ms
		oxygenSensor := fluctuate(90, 100)    // Oxygen Sensor: 90 - 100%
		fuelTankLevel := fluctuate(60, 80)    // Fuel Tank Level: 60 - 80%
		throttlePosition := fluctuate(40, 60) // Throttle Position: 40 - 60%
		engineRPM := fluctuate(2500, 3000)    // Engine RPM: 2500 - 3000

		// Send fluctuating sensor data frames to the CAN bus
		tx.TransmitFrame(context.Background(), can.Frame{ID: 0x200, Length: 8, Data: [8]byte{byte(engineTemp >> 8), byte(engineTemp & 0xFF)}})
		tx.TransmitFrame(context.Background(), can.Frame{ID: 0x201, Length: 8, Data: [8]byte{byte(injectorTiming >> 8), byte(injectorTiming & 0xFF)}})
		tx.TransmitFrame(context.Background(), can.Frame{ID: 0x202, Length: 8, Data: [8]byte{byte(oxygenSensor)}})
		tx.TransmitFrame(context.Background(), can.Frame{ID: 0x203, Length: 8, Data: [8]byte{byte(fuelTankLevel)}})
		tx.TransmitFrame(context.Background(), can.Frame{ID: 0x204, Length: 8, Data: [8]byte{byte(throttlePosition)}})
		tx.TransmitFrame(context.Background(), can.Frame{ID: 0x205, Length: 8, Data: [8]byte{byte(engineRPM >> 8), byte(engineRPM & 0xFF)}})

		time.Sleep(1 * time.Second) // Simulate a delay between sensor readings
	}
}

// main function initializes the ECU and starts the listener.
func main() {
	log.Println("Opening RX CAN interface. . .")

	ctx := context.Background()
	conn, err := socketcan.DialContext(ctx, "can", "vcan0")
	if err != nil {
		log.Fatalln("failed to connect to vcan0:", err)
	}
	defer conn.Close()

	log.Println("Listening on RX vCAN interface...")
	recv := socketcan.NewReceiver(conn)

	for recv.Receive() {
		frame := recv.Frame()

		if frame.Length < 8 {
			log.Printf("Frame ID 0x%x ignored: DLC less than 8 bytes", frame.ID)
			continue
		}

		dataFrame := hex.EncodeToString(frame.Data[:frame.Length])
		dataHex, err := hex.DecodeString(dataFrame)
		if err != nil {
			log.Println("Failed to decode paylod into string:", err)
			continue
		}

		dataStr := string(dataHex)

		// Handle engine on/off command
		if frame.ID == 0x100 && CAN_DBC[0x100].DataLen == 8 {
			engineStatus := frame.Data[0] == 1
			simulationMux.Lock()
			if engineStatus && !engineOn {
				engineOn = true
				go simulateSensors(ctx) // Start sensor simulation
			} else if !engineStatus && engineOn {
				engineOn = false
			}
			simulationMux.Unlock()
		}

		// Log received CAN messages for reference
		if msg, ok := CAN_DBC[frame.ID]; ok && msg.DataLen == 8 {
			log.Printf("%03x		[%d]	%v		'%s'	'%s'", frame.ID, frame.Length, frame.Data, dataStr, msg.Decode(frame.Data[:msg.DataLen]))
			continue
		}

		log.Printf("%03x		[%d]	%v		'%s'", frame.ID, frame.Length, frame.Data, dataStr)
	}
}
