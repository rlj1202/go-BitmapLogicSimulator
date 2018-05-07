package main

import (
	"errors"
	"image"
	"image/draw"
	_ "image/png"
	"log"
	"os"
	"path"
	"runtime"
	"strings"

	"github.com/fsnotify/fsnotify"
	"github.com/go-gl/gl/v4.1-core/gl"
	"github.com/go-gl/glfw/v3.2/glfw"
	"github.com/go-gl/mathgl/mgl32"
	"github.com/rlj1202/go-BitmapLogicSimulator"
)

const (
	vertexShader = `
	#version 410

	uniform mat4 projection;
	uniform mat4 scale;
	uniform mat4 cameraLoc;

	layout (location = 0) in vec2 position;
	layout (location = 1) in vec2 texCoord;

	out vec2 fragTexCoord;

	void main() {
		gl_Position = projection * scale * cameraLoc * vec4(position, 0, 1);
		fragTexCoord = texCoord;
	}
	`

	fragmentShader = `
	#version 410

	uniform sampler2D tex;
	uniform sampler2D tex2;

	in vec2 fragTexCoord;

	out vec4 color;

	void main() {
		vec4 back = texture2D(tex, fragTexCoord);
		vec4 overlay = back * texture2D(tex2, fragTexCoord);

		color = mix(back, overlay, 0.7);
	}
	`
)

const (
	WINDOW_WIDTH  = 800
	WINDOW_HEIGHT = 600
	WINDOW_TITLE  = "go-BitmapLogicSimulator by rlj1202"
)

var watcher *fsnotify.Watcher

var simulator *gobls.Simulator

var programId uint32

var texId uint32

var overlayPBO uint32
var overlayTex uint32

var cameraZoom float32 = 1.0
var cameraX float32
var cameraY float32

var MMB bool
var prevCursorXPos float64
var prevCursorYPos float64

var mouseInteracting bool
var mouseXIdx int
var mouseYIdx int

func main() {
	c, err := loadConfig("config.json")
	defer saveConfig("config.json", c)
	if err != nil {
		panic(err)
	}

	log.Printf("config : %v\n", *c)

	imgFile, err := os.Open(c.FileName)
	if err != nil {
		panic(err)
	}

	watcher, err := fsnotify.NewWatcher()
	defer watcher.Close()
	if err != nil {
		panic(err)
	}
	watcher.Add(path.Dir(c.FileName))

	runtime.LockOSThread()

	// init glfw
	err = glfw.Init()
	if err != nil {
		panic(err)
	}
	defer glfw.Terminate()

	// init window
	glfw.WindowHint(glfw.Resizable, glfw.True)
	glfw.WindowHint(glfw.ContextVersionMajor, 4)
	glfw.WindowHint(glfw.ContextVersionMinor, 1)
	glfw.WindowHint(glfw.OpenGLProfile, glfw.OpenGLCoreProfile)
	glfw.WindowHint(glfw.OpenGLForwardCompatible, glfw.True)
	window, err := glfw.CreateWindow(WINDOW_WIDTH, WINDOW_HEIGHT, WINDOW_TITLE, nil, nil)
	if err != nil {
		panic(err)
	}

	window.SetSizeCallback(sizeCallback)
	window.SetScrollCallback(scrollCallback)
	window.SetMouseButtonCallback(mouseButtonCallback)
	window.SetCursorPosCallback(cursorPosCallback)
	window.SetKeyCallback(keyCallback)

	window.MakeContextCurrent()
	glfw.SwapInterval(1)

	// init openGL
	err = gl.Init()
	if err != nil {
		panic(err)
	}

	version := gl.GoStr(gl.GetString(gl.VERSION))
	log.Printf("OpenGL version : %s\n", version)

	log.Println("load shader")

	// load shader
	vertexShaderId, err := loadShader(vertexShader, gl.VERTEX_SHADER)
	if err != nil {
		panic(err)
	}
	fragmentShaderId, err := loadShader(fragmentShader, gl.FRAGMENT_SHADER)
	if err != nil {
		panic(err)
	}

	programId = gl.CreateProgram()
	gl.AttachShader(programId, vertexShaderId)
	gl.AttachShader(programId, fragmentShaderId)
	gl.LinkProgram(programId)
	gl.UseProgram(programId)

	texLoc := gl.GetUniformLocation(programId, gl.Str("tex\x00"))
	gl.Uniform1i(texLoc, 0)
	tex2Loc := gl.GetUniformLocation(programId, gl.Str("tex2\x00"))
	gl.Uniform1i(tex2Loc, 1)

	log.Println("create vao")

	// create vao
	positions := []float32{
		-0.5, 0.5,
		0.5, 0.5,
		0.5, -0.5,
		-0.5, 0.5,
		0.5, -0.5,
		-0.5, -0.5,
	}
	texCoords := []float32{
		0, 0,
		1, 0,
		1, 1,
		0, 0,
		1, 1,
		0, 1,
	}

	vao := uint32(0)
	gl.GenVertexArrays(1, &vao)
	gl.BindVertexArray(vao)

	positionBuf := uint32(0)
	gl.GenBuffers(1, &positionBuf)
	gl.BindBuffer(gl.ARRAY_BUFFER, positionBuf)
	gl.BufferData(gl.ARRAY_BUFFER, 4*len(positions), gl.Ptr(positions), gl.STATIC_DRAW)
	gl.VertexAttribPointer(0, 2, gl.FLOAT, false, 0, nil)
	gl.EnableVertexAttribArray(0)

	texCoordBuf := uint32(0)
	gl.GenBuffers(1, &texCoordBuf)
	gl.BindBuffer(gl.ARRAY_BUFFER, texCoordBuf)
	gl.BufferData(gl.ARRAY_BUFFER, 4*len(texCoords), gl.Ptr(texCoords), gl.STATIC_DRAW)
	gl.VertexAttribPointer(1, 2, gl.FLOAT, false, 0, nil)
	gl.EnableVertexAttribArray(1)

	log.Println("create simulation")

	// create simulation, overlay PBO, overlay texture
	simulator = gobls.NewSimulator()

	log.Println("process image")

	// load image
	loadImage(imgFile)
	imgFile.Close()

	// start simulation
	width, height := window.GetSize()
	updateProjectionMat(programId, float32(width), float32(height)/float32(width))
	gl.ClearColor(0.1, 0.1, 0.1, 1)
	for !window.ShouldClose() {
		glfw.PollEvents()

		gl.Clear(gl.COLOR_BUFFER_BIT)

		select {
		case event := <-watcher.Events:
			if event.Name == c.FileName {
				if event.Op&fsnotify.Create == fsnotify.Create || event.Op&fsnotify.Write == fsnotify.Write {
					log.Println("file refreshed.")

					imgFile, err := os.Open(event.Name)
					if err == nil {
						loadImage(imgFile)
						imgFile.Close()
					}
				}
			}
		case err = <-watcher.Errors:
			log.Printf("file watcher err : %v\n", err)
		default:
		}

		updateOverlayTex()

		gl.ActiveTexture(gl.TEXTURE0)
		gl.BindTexture(gl.TEXTURE_2D, texId)
		gl.ActiveTexture(gl.TEXTURE1)
		gl.BindTexture(gl.TEXTURE_2D, overlayTex)
		gl.DrawArrays(gl.TRIANGLE_STRIP, 0, 6)

		for i := 0; i < c.SimulationsPerFrame; i++ {
			simulator.Simulate()
		}

		window.SwapBuffers()
	}
}

func loadImage(imgFile *os.File) {
	img, _, err := image.Decode(imgFile)
	if err != nil {
		panic(err)
	}

	if texId == 0 {
		gl.GenTextures(1, &texId)

		log.Println("gen texture name.")
	}
	err = loadTexture(img)
	if err != nil {
		panic(err)
	}

	simulator.LoadImage(img)

	width, height := simulator.Size()

	if overlayPBO == 0 {
		gl.GenBuffers(1, &overlayPBO)

		log.Println("gen overlay PBO name.")
	}
	if overlayTex == 0 {
		gl.GenTextures(1, &overlayTex)

		log.Println("gen overlay texture name.")
	}

	gl.BindBuffer(gl.PIXEL_UNPACK_BUFFER, overlayPBO)
	gl.BufferData(gl.PIXEL_UNPACK_BUFFER, width*height*4, nil, gl.STREAM_DRAW)
	gl.BindBuffer(gl.PIXEL_UNPACK_BUFFER, 0)

	gl.BindTexture(gl.TEXTURE_2D, overlayTex)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, int32(width), int32(height), 0, gl.RGBA, gl.UNSIGNED_BYTE, nil)
	gl.BindTexture(gl.TEXTURE_2D, 0)

	updateScaleMat(float32(width), float32(height), cameraZoom)
	updateCameraLocMat()
}

func updateScaleMat(x, y, zoom float32) {
	log.Printf("update scale mat : x = %f, y = %f, zoom = %f\n", x, y, zoom)
	scaleLoc := gl.GetUniformLocation(programId, gl.Str("scale\x00"))
	scaleMat := mgl32.Scale3D(x*zoom, y*zoom, 1)
	gl.UniformMatrix4fv(scaleLoc, 1, false, &scaleMat[0])
}

func updateCameraLocMat() {
	cameraLocLoc := gl.GetUniformLocation(programId, gl.Str("cameraLoc\x00"))
	cameraLocMat := mgl32.Translate3D(cameraX, -cameraY, 0)
	gl.UniformMatrix4fv(cameraLocLoc, 1, false, &cameraLocMat[0])
}

func updateOverlayTex() {
	width, height := simulator.Size()

	// update PBO
	gl.BindBuffer(gl.PIXEL_UNPACK_BUFFER, overlayPBO)
	overlayPBOPtr := gl.MapBuffer(gl.PIXEL_UNPACK_BUFFER, gl.WRITE_ONLY)
	if overlayPBOPtr != nil {
		overlayPBOSlice := (*[1 << 30]byte)(overlayPBOPtr)[:width*height*4 : width*height*4]

		simulator.PerPixel(func(x, y int, state bool) {
			index := x + y*width
			var value byte
			if state {
				value = 255
			} else {
				value = 0
			}
			overlayPBOSlice[index*4] = value
			overlayPBOSlice[index*4+1] = value
			overlayPBOSlice[index*4+2] = value
			overlayPBOSlice[index*4+3] = 255
		})

		success := gl.UnmapBuffer(gl.PIXEL_UNPACK_BUFFER)
		if !success {
			log.Println("There was a problem unmapping PBO.")
		}
	}
	gl.BindBuffer(gl.PIXEL_UNPACK_BUFFER, 0)

	// unpack PBO to overlay texture
	gl.BindBuffer(gl.PIXEL_UNPACK_BUFFER, overlayPBO)
	gl.BindTexture(gl.TEXTURE_2D, overlayTex)
	gl.TexSubImage2D(gl.TEXTURE_2D, 0, 0, 0, int32(width), int32(height), gl.RGBA, gl.UNSIGNED_BYTE, gl.PtrOffset(0))
	gl.BindTexture(gl.TEXTURE_2D, 0)
	gl.BindBuffer(gl.PIXEL_UNPACK_BUFFER, 0)
}

func updateProjectionMat(shaderProgram uint32, width, ratio float32) {
	height := width * ratio
	log.Printf("Set size : %f, %f\n", width, height)

	hw := width / 2.0
	hh := height / 2.0
	projectionLoc := gl.GetUniformLocation(programId, gl.Str("projection\x00"))
	projectionMat := mgl32.Ortho2D(-hw, hw, -hh, hh)
	gl.ProgramUniformMatrix4fv(shaderProgram, projectionLoc, 1, false, &(projectionMat[0]))
}

func loadTexture(img image.Image) error {
	gl.BindTexture(gl.TEXTURE_2D, texId)

	rgba := image.NewRGBA(img.Bounds())

	if rgba.Stride != rgba.Rect.Size().X*4 {
		return errors.New("Unsupported stride.")
	}
	draw.Draw(rgba, rgba.Bounds(), img, image.Point{0, 0}, draw.Src)

	gl.TexImage2D(gl.TEXTURE_2D, 0, gl.RGBA, int32(img.Bounds().Dx()), int32(img.Bounds().Dy()), 0, gl.RGBA, gl.UNSIGNED_BYTE, gl.Ptr(rgba.Pix))

	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_S, gl.REPEAT)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_WRAP_T, gl.REPEAT)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MAG_FILTER, gl.NEAREST)
	gl.TexParameteri(gl.TEXTURE_2D, gl.TEXTURE_MIN_FILTER, gl.LINEAR)

	return nil
}

func loadShader(rawSource string, shaderType uint32) (uint32, error) {
	rawSource += "\x00"
	shader := gl.CreateShader(shaderType)

	source, free := gl.Strs(rawSource)
	gl.ShaderSource(shader, 1, source, nil)
	free()
	gl.CompileShader(shader)

	var status int32
	gl.GetShaderiv(shader, gl.COMPILE_STATUS, &status)
	if status == gl.FALSE {
		var logLength int32
		gl.GetShaderiv(shader, gl.INFO_LOG_LENGTH, &logLength)

		log := strings.Repeat("\x00", int(logLength+1))
		gl.GetShaderInfoLog(shader, logLength, nil, gl.Str(log))

		return 0, errors.New("Failed to compile shader : " + log)
	}

	return shader, nil
}

func sizeCallback(w *glfw.Window, width, height int) {
	gl.Viewport(0, 0, int32(width), int32(height))
	updateProjectionMat(programId, float32(width), float32(height)/float32(width))
}

func scrollCallback(w *glfw.Window, xoff, yoff float64) {
	width, height := simulator.Size()

	cameraZoom += float32(yoff / 20.0)
	if cameraZoom < 0.5 {
		cameraZoom = 0.5
	}
	updateScaleMat(float32(width), float32(height), cameraZoom)
}

func cursorToImagePoint(w *glfw.Window, x, y float64) (int, int) {
	screenWidth, screenHeight := w.GetSize()
	simWidth, simHeight := simulator.Size()

	oriX := (x-float64(screenWidth)/2)/float64(simWidth)/float64(cameraZoom) - float64(cameraX)
	oriY := (y-float64(screenHeight)/2)/float64(simHeight)/float64(cameraZoom) - float64(cameraY)

	xIdx := int((oriX + 0.5) * float64(simWidth))
	yIdx := int((oriY + 0.5) * float64(simHeight))

	return xIdx, yIdx
}

func mouseButtonCallback(w *glfw.Window, button glfw.MouseButton, action glfw.Action, mod glfw.ModifierKey) {
	x, y := w.GetCursorPos()
	xIdx, yIdx := cursorToImagePoint(w, x, y)
	simWidth, simHeight := simulator.Size()

	if button == glfw.MouseButtonMiddle {
		switch action {
		case glfw.Press:
			MMB = true
			prevCursorXPos, prevCursorYPos = w.GetCursorPos()
		case glfw.Release:
			MMB = false
		}
	} else if button == glfw.MouseButtonLeft {
		if 0 <= xIdx && xIdx < simWidth && 0 <= yIdx && yIdx < simHeight {
			if action == glfw.Press {
				mouseInteracting = true
				mouseXIdx = xIdx
				mouseYIdx = yIdx

				simulator.Set(xIdx, yIdx, true)
			} else if action == glfw.Release {
				if mouseInteracting {
					mouseInteracting = false
					simulator.Set(mouseXIdx, mouseYIdx, false)
				}
			}
		}
	} else if button == glfw.MouseButtonRight {
		if 0 <= xIdx && xIdx < simWidth && 0 <= yIdx && yIdx < simHeight {
			if action == glfw.Press {
				state := simulator.Get(xIdx, yIdx)
				if state {
					simulator.Set(xIdx, yIdx, false)
				} else {
					simulator.Set(xIdx, yIdx, true)
				}
			}
		}
	}
}

func cursorPosCallback(w *glfw.Window, xpos, ypos float64) {
	if MMB {
		width, height := simulator.Size()
		dx := xpos - prevCursorXPos
		dy := ypos - prevCursorYPos

		cameraX += float32(dx) / (float32(width) * cameraZoom)
		cameraY += float32(dy) / (float32(height) * cameraZoom)

		prevCursorXPos = xpos
		prevCursorYPos = ypos

		updateCameraLocMat()
	}
}

func keyCallback(w *glfw.Window, key glfw.Key, scancode int, action glfw.Action, mods glfw.ModifierKey) {
	if glfw.KeyKP0 <= key && key <= glfw.KeyKP9 && action == glfw.Press {
		width, height := simulator.Size()

		switch key {
		case glfw.KeyKP0:
			cameraZoom = 1.0
			cameraX = 0.0
			cameraY = 0.0
		case glfw.KeyKP1:
			cameraZoom = 1.0
		case glfw.KeyKP2:
			cameraZoom = 2.0
		case glfw.KeyKP3:
			cameraZoom = 3.0
		case glfw.KeyKP4:
			cameraZoom = 4.0
		case glfw.KeyKP5:
			cameraZoom = 5.0
		}

		updateScaleMat(float32(width), float32(height), cameraZoom)
		updateCameraLocMat()
	}
}
