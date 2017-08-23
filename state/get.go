package state

import (
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"

	"github.com/byuoitav/av-api/base"
	se "github.com/byuoitav/av-api/statusevaluators"
	"github.com/byuoitav/configuration-database-microservice/structs"
	"github.com/byuoitav/event-router-microservice/eventinfrastructure"
	"github.com/fatih/color"
)

func GenerateStatusCommands(room structs.Room, commandMap map[string]se.StatusEvaluator) ([]se.StatusCommand, error) {

	color.Set(color.FgHiCyan)
	log.Printf("[state] generating status commands...")
	color.Unset()

	var output []se.StatusCommand

	for _, possibleEvaluator := range room.Configuration.Evaluators {

		if strings.HasPrefix(possibleEvaluator.EvaluatorKey, se.FLAG) {

			currentEvaluator := se.STATUS_EVALUATORS[possibleEvaluator.EvaluatorKey]

			devices, err := currentEvaluator.GetDevices(room)
			if err != nil {
				return []se.StatusCommand{}, err
			}

			commands, err := currentEvaluator.GenerateCommands(devices)
			if err != nil {
				return []se.StatusCommand{}, err
			}

			output = append(output, commands...)
		}
	}

	return output, nil
}

func RunStatusCommands(commands []se.StatusCommand) (outputs []se.StatusResponse, err error) {

	color.Set(color.FgHiCyan)
	log.Printf("[state] running status commands...")
	color.Unset()

	if len(commands) == 0 {
		err = errors.New("no commands")
		return
	}

	//map device names to commands
	commandMap := make(map[string][]se.StatusCommand)

	log.Printf("[state] building device map\n\n")
	for _, command := range commands {

		log.Printf("[state] command: %s against device %s, destination device: %s, parameters: %v", command.Action.Name, command.Device.Name, command.DestinationDevice.Device.Name, command.Parameters)
		_, present := commandMap[command.Device.Name]
		if !present {
			commandMap[command.Device.Name] = []se.StatusCommand{command}
			//	log.Printf("Device %s identified", command.Device.Name)
		} else {
			commandMap[command.Device.Name] = append(commandMap[command.Device.Name], command)
		}

	}

	log.Printf("[state] creating channel")
	channel := make(chan []se.StatusResponse, len(commandMap))
	var group sync.WaitGroup

	for _, deviceCommands := range commandMap {
		group.Add(1)
		go issueCommands(deviceCommands, channel, &group)

		log.Printf("Commands getting issued: \n\n")

		for _, command := range deviceCommands {
			log.Printf("Command: %s against device %s, destination device: %s, parameters: %v", command.Action.Name, command.Device.Name, command.DestinationDevice.Device.Name, command.Parameters)
		}
	}

	log.Printf("[state] Waiting for commands to issue...")
	group.Wait()
	log.Printf("[state] Done. Closing channel...")
	close(channel)

	for outputList := range channel {
		for _, output := range outputList {
			if output.ErrorMessage != nil {
				log.Printf("[error] problem querying status of device: %s: %s", output.SourceDevice.Name, *output.ErrorMessage)
				cause := eventinfrastructure.INTERNAL
				message := *output.ErrorMessage
				message = "Error querying status for destination: " + output.DestinationDevice.Name + ": " + message
				base.PublishError(message, cause)
			}
			log.Printf("[state] Appending status: %v of %s to output", output.Status, output.DestinationDevice.Name)
			outputs = append(outputs, output)
		}
	}
	return
}

func EvaluateResponses(responses []se.StatusResponse) (base.PublicRoom, error) {

	color.Set(color.FgHiCyan)
	log.Printf("[state] Evaluating responses...")
	//==========================================================================================================================================================================================================
	log.Printf("Responses: ")

	for _, response := range responses {

		fmt.Printf("%s -> %s, %s\n", response.Status, response.DestinationDevice.Name, response.Generator)
	}

	//==========================================================================================================================================================================================================
	color.Unset()

	var AudioDevices []base.AudioDevice
	var Displays []base.Display

	//make our array of Statuses by device
	responsesByDestinationDevice := make(map[string]se.Status)
	for _, resp := range responses {

		for key, value := range resp.Status {

			log.Printf("[state] Checking generator: %s", resp.Generator)
			k, v, err := se.STATUS_EVALUATORS[resp.Generator].EvaluateResponse(key, value, resp.SourceDevice, resp.DestinationDevice)
			if err != nil {

				color.Set(color.FgHiRed, color.Bold)
				log.Printf("[state] problem procesing the response %v - %v with evaluator %v: %s",
					key, value, resp.Generator, err.Error())
				color.Unset()
				continue
			}

			if _, ok := responsesByDestinationDevice[resp.DestinationDevice.GetFullName()]; ok {
				responsesByDestinationDevice[resp.DestinationDevice.GetFullName()].Status[k] = v
			} else {
				newMap := make(map[string]interface{})
				newMap[k] = v
				statusForDevice := se.Status{
					Status:            newMap,
					DestinationDevice: resp.DestinationDevice,
				}
				responsesByDestinationDevice[resp.DestinationDevice.GetFullName()] = statusForDevice
				log.Printf("[state] Adding Device %v to the map", resp.DestinationDevice.GetFullName())
			}
		}
	}

	//==================================================================================================================================================================================================================

	color.Set(color.FgHiCyan, color.Bold)
	for _, status := range responsesByDestinationDevice {

		fmt.Printf("Device: %s, status: %s, display: %t, audio: %t\n", status.DestinationDevice.Name, status.Status, status.DestinationDevice.Display, status.DestinationDevice.AudioDevice)
	}

	color.Unset()
	//==================================================================================================================================================================================================================

	for _, v := range responsesByDestinationDevice {
		if v.DestinationDevice.AudioDevice {
			audioDevice, err := processAudioDevice(v)
			if err == nil {
				AudioDevices = append(AudioDevices, audioDevice)
			}
		}
		if v.DestinationDevice.Display {

			display, err := processDisplay(v)
			if err == nil {
				Displays = append(Displays, display)
			}
		}
	}

	return base.PublicRoom{Displays: Displays, AudioDevices: AudioDevices}, nil
}
