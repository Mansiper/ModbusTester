package main

/* It was written for Raspberry Pi and tested in Windows and Ubuntu just to check working. Works well */

import (
	"io"
	"os"
	"fmt"
	"log"
	"math"
	"time"
	"bufio"
	"strings"
	"runtime"
	"strconv"
	"encoding/json"
	"github.com/tarm/goserial"
)

type Settings struct {
	Port string
	Baud int
	Timeout time.Duration
}

const (
	cRespSize = 20
	confFile = "port.conf"
)
var (
	mb io.ReadWriteCloser
	resp []byte
)

//==================================================================================================

func Round(val float64, roundOn float64, places int) (newVal float64) {
	//Got from https://gist.github.com/DavidVaini/10308388
	var round float64
	pow := math.Pow(10, float64(places))
	digit := pow * val
	_, div := math.Modf(digit)
	if div >= roundOn {
		round = math.Ceil(digit)
	} else {
		round = math.Floor(digit)
	}
	newVal = round / pow
	return
}

//--------------------------------------------------------------------------------------------------

func CalcCRC(buf []byte, ln byte) uint16 {
	var (
		i byte
		res, x uint16
		ff bool
	)

	res = 0xFFFF
	for i = 0; i < ln; i++ {
		x = res ^ uint16(buf[i])
		for j := 1; j <= 8; j++ {
			ff = (x & 0x0001) == 1
			x = (x >> 1) & 0x7FFF
			if ff { x = x ^ 0xA001 }
		}
		res = x
	}

	return res
}

func BytesToFloat(b []byte) float32 {
	var b0, b1, b2, b3 uint32
	b0 = uint32(b[0])
	b1 = uint32(b[1]) * 0x100
	b2 = uint32(b[2]) * 0x10000
	b3 = uint32(b[3]) * 0x1000000
	return math.Float32frombits(b0 + b1 + b2 + b3)
}

func FloatToBytes(f float32) []byte {
	var b []byte
	v := math.Float32bits(f)
	b = append(b, byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v))
	return b
}

func CheckResponse(req, res []byte, resSize int) bool {
	return (req[0] == res[0]) && (req[1] == res[1] || req[1] == res[1]-128) &&
		res[resSize-2] == byte(CalcCRC(res, byte(resSize-2))) &&
		res[resSize-1] == byte(CalcCRC(res, byte(resSize-2)) >> 8)
}

//--------------------------------------------------------------------------------------------------

func Send(request []byte, respSize int) bool {
	var (
		n, x int
		err error
	)
	buf := make([]byte, respSize)

	//Write in port
	n, err = mb.Write(request)
	if err != nil {
		fmt.Println(err)
		return false
	}

	for i := 0; i < respSize; i++ { resp[i] = 0 }

	//Reading
	//For Windows
	if runtime.GOOS == "windows" {
		for i := 1; i <= 3; i++ {	//Three attempts
			n, err = mb.Read(buf)
			if n > 5 {
				for j := 0; j < n; j++ {
					//if x == 0 && buf[j] != request[0] { continue }	//First byte is address
					resp[j] = buf[j]
					if j >= cRespSize-1 { break }
				}
				break
			}
		}
		if err != nil {
			fmt.Println(err)
			return false
		}
	//For Linux
	} else {
		for i := 1; i <= 5; i++ {	//Three attempts
			n, err = mb.Read(buf)
			if n > 0 {
				for j := 0; j < n; j++ {
					if x == 0 && buf[j] != request[0] { continue }	//First byte is address
					if x >= len(resp) || j >= len(buf) { break }	//First responses can be empty in Raspbian
					resp[x] = buf[j]
					x++
					if x >= cRespSize-1 { break }
				}
			} else if err != nil { break }	//Reading till the end or next response will catch these bytes
		}
	}

	return true
}

func MainWork() {
	var (
		res bool
		crc uint16
		reqstr string
		req []byte
		err error
		respSize int = 8
	)

	for {
		reqstr = ""
		fmt.Println("Enter request using spaces without CRC:")
		reader := bufio.NewReader(os.Stdin)
		if reqstr, err = reader.ReadString('\n'); err != nil {
			fmt.Println(err)
			continue
		}
		//Parsing
		req = []byte{}
		reqstr = strings.TrimSuffix(reqstr, "\r\n")	//For Linux
		reqstr = strings.TrimSuffix(reqstr, "\n")		//For Windows
		if reqstr == "exit" { break }
		for _, v := range strings.Split(reqstr, " ") {
			i, err := strconv.Atoi(v)
			if err != nil {
				fmt.Println(err)
				continue
			}
			req = append(req, byte(i))
		}
		//Add CRC
		crc = CalcCRC(req, byte(len(req)))
		req = append(req, byte(crc), byte(crc >> 8))
		fmt.Print("Request:")
		for i := 0; i < len(req); i++ { fmt.Print(" ", strconv.Itoa(int(req[i]))) }
		fmt.Print("\n")

		switch req[1] {
			case 0x01, 0x02, 0x03, 0x04:
				respSize = 5 + (int(req[5]) + int(req[4]) * 256) * 2
			case 0x05, 0x06, 0x0F, 0x10:
				respSize = 8
		}

		//Send
		res = Send(req, respSize)
		if res { res = CheckResponse(req, resp, respSize) }
		if resp[1] > 128 {
			fmt.Print("Error:")
		} else {
			fmt.Print("Response:")
		}
		for i := 0; i < int(respSize); i++ {
			fmt.Print(" ", strconv.Itoa(int(resp[i])))
		}
		fmt.Print("\n")
		if resp[1] > 128 {
			switch resp[2] {
				case 1:	fmt.Println("Function code received in the query is not recognized or allowed by slave")
				case 2:	fmt.Println("Data address of some or all the required entities are not allowed or do not exist in slave")
				case 3:	fmt.Println("Value is not accepted by slave")
				case 4:	fmt.Println("Unrecoverable error occurred while slave was attempting to perform requested action")
				case 5:	fmt.Println("Slave has accepted request and is processing it, but a long duration of time is required")
				case 6:	fmt.Println("Slave is engaged in processing a long-duration command. Master should retry later")
				case 7:	fmt.Println("Slave cannot perform the programming functions. Master should request diagnostic or error information from slave")
				case 8:	fmt.Println("Slave detected a parity error in memory. Master can retry the request, but service may be required on the slave device")
				case 10:fmt.Println("Specialized for Modbus gateways. Indicates a misconfigured gateway")
				case 11:fmt.Println("Specialized for Modbus gateways. Sent when slave fails to respond")
			}
		}
		fmt.Println("Result:", res, "\n")
	}
}

//--------------------------------------------------------------------------------------------------

func main() {
	var (
		err error
		sets Settings
	)
	resp = make([]byte, cRespSize)

	//Reading settings
	if _, err = os.Stat(confFile); err == nil {
		file, err := os.Open(confFile)
		if err == nil {
			err = json.NewDecoder(file).Decode(&sets)
		}
	}
	//Default settings
	if err != nil {
		fmt.Println(err)
		if runtime.GOOS == "windows" {
			sets.Port = "COM1"
		} else {
			sets.Port = "/dev/ttyUSB0"
		}
		sets.Baud = 19200
		sets.Timeout = 50
	}
	fmt.Println("Port settings:", sets.Port, sets.Baud, sets.Timeout)

	//Open port
	port:= new(serial.Config)
	port.Name = sets.Port
	port.Baud = sets.Baud
	port.ReadTimeout = time.Millisecond * sets.Timeout
	mb, err = serial.OpenPort(port)
		if err != nil { log.Fatal(err) }
	defer mb.Close()

	MainWork();
}
