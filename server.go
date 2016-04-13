package main

import (
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	"github.com/labstack/echo"
	"github.com/labstack/echo/engine/fasthttp"
	"github.com/ziutek/telnet"
)

type fusionResponse struct {
	APIRooms []fusionRoom `json:"API_Rooms"`
}

type fusionRoom struct {
	RoomID   string
	RoomName string
	Symbols  []fusionSymbol
}

type fusionSymbol struct {
	ProcessorName string
	ConnectInfo   string
}

type soapEnvelope struct {
	XMLName struct{} `xml:"http://schemas.xmlsoap.org/soap/envelope/ Envelope"`
	Body    soapBody
}

type soapBody struct {
	XMLName  struct{} `xml:"http://schemas.xmlsoap.org/soap/envelope/ Body"`
	Contents []byte   `xml:",innerxml"`
}

type schedulingBuildingsRequest struct {
	XMLName  struct{} `xml:"http://DEA.EMS.API.Web.Service/ GetBuildings"`
	Username string
	Password string
}

type schedulingBuildingsResponse struct {
	XMLName struct{} `xml:"http://DEA.EMS.API.Web.Service/ GetBuildings"`
	Result  string   `xml:"GetBuildingsResult"`
}

type room struct {
	Hostname  string
	Address   string
	Available bool
}

func checkErr(err error) {
	if err != nil {
		panic(err) // Don't forget your towel
	}
}

func health(c echo.Context) error {
	return c.String(http.StatusOK, "Uh, we had a slight weapons malfunction, but uh... everything's perfectly all right now. We're fine. We're all fine here now, thank you. How are you?")
}

func soapEncode(contents interface{}) ([]byte, error) {
	data, err := xml.MarshalIndent(contents, "    ", "  ")
	if err != nil {
		return nil, err
	}
	data = append([]byte("\n"), data...)
	env := soapEnvelope{Body: soapBody{Contents: data}}
	return xml.MarshalIndent(&env, "", "  ")
}

func soapDecode(data []byte, contents interface{}) error {
	env := soapEnvelope{Body: soapBody{}}
	err := xml.Unmarshal(data, &env)
	if err != nil {
		return err
	}
	return xml.Unmarshal(env.Body.Contents, contents)
}

func getTelnetOutput(address string, port string, command string) string {
	t, err := telnet.Dial("tcp", address+":"+port)
	checkErr(err)

	t.SetUnixWriteMode(true) // Convert any '\n' (LF) to '\r\n' (CR LF)

	command = command + "\nhostname" // Send two commands so we get a second prompt to use as a delimiter
	buf := make([]byte, len(command)+1)
	copy(buf, command)
	buf[len(command)] = '\n'
	_, err = t.Write(buf)
	checkErr(err)

	t.SkipUntil("TSW-750>") // Skip to the first prompt delimiter
	var output []byte
	output, err = t.ReadUntil("TSW-750>") // Read until the second prompt delimiter (provided by sending two commands in sendCommand)
	checkErr(err)

	t.Close() // Close the telnet session

	output = output[:len(output)-10] // Ghetto trim the prompt off the response

	return string(output)
}

func fusionRequest(requestType string, url string) []byte {
	client := &http.Client{}
	req, err := http.NewRequest(requestType, url, nil)
	checkErr(err)

	req.Header.Add("Content-Type", "application/json")
	resp, err := client.Do(req)
	checkErr(err)

	body, err := ioutil.ReadAll(resp.Body)
	checkErr(err)

	return body
}

func getRooms(c echo.Context) error {
	response := fusionRequest("GET", "http://lazyeye.byu.edu/fusion/apiservice/rooms/")
	return c.String(http.StatusOK, string(response)) // MAKE SURE YOU HAVE THE TRAILING SLASH
}

func getRoomByName(c echo.Context) error {
	// Get the room's ID from its name
	response := fusionRequest("GET", "http://lazyeye.byu.edu/fusion/apiservice/rooms/?search="+c.Param("room"))
	rooms := fusionResponse{}
	err := json.Unmarshal(response, &rooms)
	checkErr(err)

	// Get info about the room using its ID
	response = fusionRequest("GET", "http://lazyeye.byu.edu/fusion/apiservice/rooms/"+rooms.APIRooms[0].RoomID)
	rooms = fusionResponse{}
	err = json.Unmarshal(response, &rooms)
	checkErr(err)

	hostname := rooms.APIRooms[0].Symbols[0].ProcessorName
	address := rooms.APIRooms[0].Symbols[0].ConnectInfo

	// Check room availability
	req := &schedulingBuildingsRequest{Username: os.Getenv("EMS_API_USERNAME"), Password: os.Getenv("EMS_API_PASSWORD")}
	data, err := soapEncode(&req)
	checkErr(err)
	fmt.Println("Request:")
	fmt.Println(xml.Header + string(data))
	var resp schedulingBuildingsResponse
	err = soapDecode([]byte(response), &resp)
	checkErr(err)
	fmt.Println("Response:")
	fmt.Println(resp.Result)

	roomResponse := room{Hostname: hostname, Address: address, Available: true} // Temporary debugging

	jsonResponse, _ := json.Marshal(roomResponse)
	return c.String(http.StatusOK, string(jsonResponse))
}

func main() {
	port := ":8000"
	e := echo.New()

	// Echo doesn't like doing things "magically" which means it won't auto-redirect endpoints without a trailing slash to one with a trailing slash (and vice versa) which is why endpoints are duplicated
	e.Get("/health", health)
	e.Get("/health/", health)

	e.Get("/rooms", getRooms)
	e.Get("/rooms/", getRooms)
	e.Get("/rooms/:room", getRoomByName)
	e.Get("/rooms/:room/", getRoomByName)
	// e.Get("/buildings", ...)
	// e.Get("/buildings/:building", ...)
	// e.Get("/buildings/:building/room", ...)
	// e.Get("/buildings/:building/rooms/:room", ...)
	// e.Get("/buildings/:building/rooms/:room/signals", ...)
	// e.Get("/buildings/:building/rooms/:room/signals/:signal", ...)
	//
	// e.Post("/rooms", ...)
	// e.Post("/buildings", ...)
	// e.Post("/buildings/:building/rooms/:room/signals", ...)
	//
	// e.Put("/rooms/:room", ...)
	// e.Put("/buildings/:building", ...)
	// e.Put("/buildings/:building/rooms/:room", ...)
	// e.Put("/buildings/:building/rooms/:room/signals/:signal", ...)
	//
	// e.Delete("/rooms/:room", ...)
	// e.Delete("/buildings/:building", ...)
	// e.Delete("/buildings/:building/rooms/:room", ...)
	// e.Delete("/buildings/:building/rooms/:room/signals/:signal", ...)

	fmt.Printf("AV API is listening on %s\n", port)
	e.Run(fasthttp.New(port))
}
