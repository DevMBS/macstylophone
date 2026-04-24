//go:build darwin

package hardware

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation -framework CoreFoundation

#include <CoreFoundation/CoreFoundation.h>
#include <dlfcn.h>
#include <stdlib.h>

// типы MultitouchSupport.framework
typedef struct {
    float x;
    float y;
} MTPoint;

typedef struct {
    MTPoint position;
    MTPoint velocity;
} MTVector;

// MTTouch структура
typedef struct {
    int32_t frame;
    double timestamp;
    int32_t identifier;
    int32_t state;           // 0=не нажат, 1-4=состояние нажатия, 7=поднят палец
    int32_t fingerID;
    int32_t handID;
    MTVector normalized;     // normalized position and velocity
    float size;
    int32_t zero1;
    float angle;
    float majorAxis;
    float minorAxis;
    MTVector absolute;       // абсолютная позиция
    int32_t zero2;
    int32_t zero3;
    float density;
} MTTouch;

typedef void* MTDeviceRef;
typedef void (*MTContactCallbackFunction)(MTDeviceRef device, MTTouch *touches, int numTouches, double timestamp, int frame);

static void* mtlib = NULL;
static MTDeviceRef (*pMTDeviceCreateDefault)(void) = NULL;
static void (*pMTRegisterContactFrameCallback)(MTDeviceRef, MTContactCallbackFunction) = NULL;
static void (*pMTDeviceStart)(MTDeviceRef, int) = NULL;
static void (*pMTDeviceStop)(MTDeviceRef) = NULL;
static CFArrayRef (*pMTDeviceCreateList)(void) = NULL;

#define MAX_TOUCHES 16

typedef struct {
    int fingerID;
    float x;
    float y;
    float pressure;
    int state;
    double timestamp;
} TouchData;

static TouchData touchBuffer[MAX_TOUCHES];
static int touchCount = 0;
static int newDataAvailable = 0;

// Колбэк вызываемый MultitouchSupport
static void touchCallback(MTDeviceRef device, MTTouch *touches, int numTouches, double timestamp, int frame) {
    if (numTouches > MAX_TOUCHES) {
        numTouches = MAX_TOUCHES;
    }

    for (int i = 0; i < numTouches; i++) {
        touchBuffer[i].fingerID = touches[i].identifier;
        touchBuffer[i].x = touches[i].normalized.position.x;
        touchBuffer[i].y = touches[i].normalized.position.y;
        touchBuffer[i].pressure = touches[i].size;
        touchBuffer[i].state = touches[i].state;
        touchBuffer[i].timestamp = timestamp;
    }
    touchCount = numTouches;
    newDataAvailable = 1;
}

// Инициализация фремворка MultitouchSupport
static int initMultitouch(void) {
    if (mtlib != NULL) {
        return 0;
    }

    mtlib = dlopen("/System/Library/PrivateFrameworks/MultitouchSupport.framework/MultitouchSupport", RTLD_LAZY);
    if (!mtlib) {
        return -1;
    }

    pMTDeviceCreateDefault = dlsym(mtlib, "MTDeviceCreateDefault");
    pMTRegisterContactFrameCallback = dlsym(mtlib, "MTRegisterContactFrameCallback");
    pMTDeviceStart = dlsym(mtlib, "MTDeviceStart");
    pMTDeviceStop = dlsym(mtlib, "MTDeviceStop");
    pMTDeviceCreateList = dlsym(mtlib, "MTDeviceCreateList");

    if (!pMTDeviceCreateDefault || !pMTRegisterContactFrameCallback ||
        !pMTDeviceStart || !pMTDeviceStop) {
        dlclose(mtlib);
        mtlib = NULL;
        return -2;
    }

    return 0;
}

static MTDeviceRef currentDevice = NULL;

// Старт захвата тачпада
static int startMultitouch(void) {
    if (initMultitouch() != 0) {
        return -1;
    }

    if (pMTDeviceCreateList) {
        CFArrayRef devices = pMTDeviceCreateList();
        if (devices && CFArrayGetCount(devices) > 0) {
            currentDevice = (MTDeviceRef)CFArrayGetValueAtIndex(devices, 0);
        }
    }

    if (!currentDevice) {
        currentDevice = pMTDeviceCreateDefault();
    }

    if (!currentDevice) {
        return -2;
    }

    pMTRegisterContactFrameCallback(currentDevice, touchCallback);
    pMTDeviceStart(currentDevice, 0);

    return 0;
}

// Остановка захвата
static void stopMultitouch(void) {
    if (currentDevice && pMTDeviceStop) {
        pMTDeviceStop(currentDevice);
        currentDevice = NULL;
    }
}

// Проверка новых данных
static int hasNewData(void) {
    return newDataAvailable;
}

static int getTouchCount(void) {
    return touchCount;
}

static TouchData getTouchAt(int index) {
    if (index >= 0 && index < touchCount) {
        return touchBuffer[index];
    }
    TouchData empty = {0};
    return empty;
}

static void clearNewDataFlag(void) {
    newDataAvailable = 0;
}
*/
import "C"

import (
	"context"
	"errors"
	"log"
	"sync"
	"time"
)

// TouchState определяет состояние пальца
type TouchState int

const (
	TouchStateLifted TouchState = 7
)

func (s TouchState) IsTouching() bool {
	return s >= 1 && s <= 6
}

// TouchFrame определяет сырую точку касания
type TouchFrame struct {
	FingerID  int
	X         float64
	Y         float64
	Pressure  float64
	State     TouchState
	Timestamp float64
}

// EventType теперь отражает жизненный цикл звука (ADSR)
type EventType int

const (
	EventSynthStart EventType = iota // Начало звука (Attack)
	EventSynthMove                   // Модуляция звука
	EventSynthEnd                    // Конец звука (Release)
)

func (e EventType) String() string {
	switch e {
	case EventSynthStart:
		return "SynthStart"
	case EventSynthMove:
		return "SynthMove"
	case EventSynthEnd:
		return "SynthEnd"
	default:
		return "Unknown"
	}
}

// MusicEvent - то, что полетит по WebSocket на фронтенд
type MusicEvent struct {
	Type      EventType `json:"type"`
	FingerID  int       `json:"finger_id"` // Важно для аккордов! (Polyphony)
	X         float64   `json:"x"`         // 0.0 - 1.0 (Весь тачпад)
	Y         float64   `json:"y"`         // 0.0 - 1.0 (Весь тачпад)
	Pressure  float64   `json:"pressure"`
	Timestamp float64   `json:"timestamp"`
}

// MultitouchPoller обрабатывает данные с тачпада
type MultitouchPoller struct {
	mu           sync.Mutex
	running      bool
	pollInterval time.Duration
	lastFrames   map[int]TouchFrame // Храним последние координаты для плавного Release
}

func NewMultitouchPoller(pollInterval time.Duration) *MultitouchPoller {
	return &MultitouchPoller{
		pollInterval: pollInterval,
		lastFrames:   make(map[int]TouchFrame),
	}
}

func (p *MultitouchPoller) Start() error {
	p.mu.Lock()
	defer p.mu.Unlock()

	if p.running {
		return errors.New("мультитач поллер уже запущен")
	}

	result := C.startMultitouch()
	if result != 0 {
		return errors.New("ошибка инициализации MultitouchSupport")
	}

	p.running = true
	log.Println("[multitouch] Захват всей поверхности тачпада начат")
	return nil
}

func (p *MultitouchPoller) Stop() {
	p.mu.Lock()
	defer p.mu.Unlock()

	if !p.running {
		return
	}

	C.stopMultitouch()
	p.running = false
	log.Println("[multitouch] Остановлен захват тачпада")
}

func (p *MultitouchPoller) StartEventPolling(ctx context.Context, eventChan chan<- MusicEvent) error {
	if err := p.Start(); err != nil {
		return err
	}
	go p.eventLoop(ctx, eventChan)
	return nil
}

func (p *MultitouchPoller) eventLoop(ctx context.Context, eventChan chan<- MusicEvent) {
	ticker := time.NewTicker(p.pollInterval)
	defer ticker.Stop()
	defer p.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("[multitouch] Цикл остановлен")
			return
		case <-ticker.C:
			events := p.processEvents()
			for _, event := range events {
				select {
				case eventChan <- event:
				default:
					// Канал забит, пропускаем чтобы не тормозить
				}
			}
		}
	}
}

func (p *MultitouchPoller) readFrames() []TouchFrame {
	if C.hasNewData() == 0 {
		return nil
	}
	count := int(C.getTouchCount())
	frames := make([]TouchFrame, 0, count)

	for i := 0; i < count; i++ {
		data := C.getTouchAt(C.int(i))
		frames = append(frames, TouchFrame{
			FingerID:  int(data.fingerID),
			X:         float64(data.x),
			Y:         float64(data.y),
			Pressure:  float64(data.pressure),
			State:     TouchState(data.state),
			Timestamp: float64(data.timestamp),
		})
	}
	C.clearNewDataFlag()
	return frames
}

// processEvents - Ядро логики. Превращает сырые касания в музыкальные команды
func (p *MultitouchPoller) processEvents() []MusicEvent {
	frames := p.readFrames()

	p.mu.Lock()
	defer p.mu.Unlock()

	var events []MusicEvent
	currentIDs := make(map[int]bool)

	// Обрабатываем пришедшие фреймы
	for _, frame := range frames {
		currentIDs[frame.FingerID] = true
		_, existed := p.lastFrames[frame.FingerID]

		if frame.State.IsTouching() {
			if !existed {
				// ПАЛЕЦ ТОЛЬКО ЧТО ОПУСТИЛСЯ (Attack)
				events = append(events, p.makeEvent(EventSynthStart, frame))
			} else {
				// ПАЛЕЦ ДВИЖЕТСЯ (Modulation)
				events = append(events, p.makeEvent(EventSynthMove, frame))
			}
			p.lastFrames[frame.FingerID] = frame
		} else if frame.State == TouchStateLifted {
			// ПАЛЕЦ ПОДНЯТ (Release)
			events = append(events, p.makeEvent(EventSynthEnd, frame))
			delete(p.lastFrames, frame.FingerID)
		}
	}

	// Страховка: если палец исчез из радара macOS без события "Lifted"
	for id, lastFrame := range p.lastFrames {
		if !currentIDs[id] {
			events = append(events, p.makeEvent(EventSynthEnd, lastFrame))
			delete(p.lastFrames, id)
		}
	}

	return events
}

func (p *MultitouchPoller) makeEvent(t EventType, f TouchFrame) MusicEvent {
	return MusicEvent{
		Type:      t,
		FingerID:  f.FingerID,
		X:         f.X, // Координаты больше не обрезаются
		Y:         f.Y, // Все 100% тачпада в нашем распоряжении
		Pressure:  f.Pressure,
		Timestamp: f.Timestamp,
	}
}
