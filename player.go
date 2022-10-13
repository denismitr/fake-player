package player

import (
	"context"
	"errors"
	"sync"
	"time"
)

var (
	ErrPlaylistEmpty  = errors.New("playlist is empty")
	ErrNotPlaying     = errors.New("not playing")
	ErrAlreadyPlaying = errors.New("already playing")
	ErrEndOfPlaylst   = errors.New("end of playlist")
)

type (
	Song struct {
		Name     string
		Duration time.Duration
	}

	playlistItem struct {
		posistion time.Duration
		song      *Song
		next      *playlistItem
		prev      *playlistItem
	}

	TickHandler func(seconds time.Duration, song Song)

	PlayerState int
	command     int

	Player interface {
		Play(context.Context) error
		OnPlayTick(TickHandler)
		Pause(context.Context) error
		Stop(context.Context) error
		Len(ctx context.Context) (int, error)
		Add(ctx context.Context, song Song) error
		Stopped() <-chan struct{}
	}

	InMemoryPlayer struct {
		mux         sync.Mutex
		playlist    *playlistItem
		current     *playlistItem
		playlistLen int
		callbacks   []TickHandler
		state       PlayerState
		commands    chan command
		stopCh      chan struct{}
	}
)

const (
	tick = time.Second
)

const (
	PlayerStateStopped PlayerState = iota
	PlayerStatePlaying
	PlayerStatePaused
)

const (
	start command = iota
	stop
	pause
	resume
	reset
	next
	prev
)

func New(songs ...Song) (*InMemoryPlayer, error) {
	p := &InMemoryPlayer{
		state:    PlayerStateStopped,
		commands: make(chan command, 3),
		stopCh:   make(chan struct{}, 3),
	}

	ctx := context.TODO()
	for _, song := range songs {
		if err := p.addUnderLock(ctx, song); err != nil {
			return nil, err
		}
	}

	return p, nil
}

func (p *InMemoryPlayer) Add(ctx context.Context, song Song) error {
	p.mux.Lock()
	defer p.mux.Unlock()

	return p.addUnderLock(ctx, song)
}

func (p *InMemoryPlayer) addUnderLock(ctx context.Context, song Song) error {
	if p.playlist == nil {
		p.playlist = &playlistItem{
			song: &song,
			next: nil,
			prev: nil,
		}
		p.playlistLen = 1
		return nil
	}

	curr := p.playlist
	for curr.next != nil {
		curr = curr.next
	}

	curr.next = &playlistItem{
		song: &song,
		next: nil,
		prev: curr,
	}

	p.playlistLen++

	return nil
}

func (p *InMemoryPlayer) Len(ctx context.Context) (int, error) {
	return p.playlistLen, nil
}

func (p *InMemoryPlayer) OnPlayTick(cb TickHandler) {
	p.mux.Lock()
	defer p.mux.Unlock()

	p.callbacks = append(p.callbacks, cb)
}

func (p *InMemoryPlayer) Play(ctx context.Context) error {
	p.mux.Lock()
	defer p.mux.Unlock()
	if p.playlist == nil {
		return ErrPlaylistEmpty
	}

	switch p.state {
	case PlayerStatePlaying:
		return ErrAlreadyPlaying
	case PlayerStateStopped:
		go p.run(ctx)
		p.commands <- start
	case PlayerStatePaused:
		p.commands <- resume
	}

	return nil
}

func (p *InMemoryPlayer) Stop(ctx context.Context) error {
	return p.doSendCommand(ctx, stop)
}

func (p *InMemoryPlayer) Pause(ctx context.Context) error {
	return p.doSendCommand(ctx, pause)
}

func (p *InMemoryPlayer) Next(ctx context.Context) error {
	return p.doSendCommand(ctx, next)
}

func (p *InMemoryPlayer) Prev(ctx context.Context) error {
	return p.doSendCommand(ctx, prev)
}

func (p *InMemoryPlayer) Stopped() <-chan struct{} {
	return p.stopCh
}

func (p *InMemoryPlayer) doSendCommand(ctx context.Context, cmd command) error {
	select {
	case p.commands <- cmd:
	case <-ctx.Done():
		return ctx.Err()
	}

	return nil
}

func (p *InMemoryPlayer) nextUnderLock() error {
	if p.current == nil {
		p.current = p.playlist
		if p.current == nil {
			return ErrPlaylistEmpty
		}
	}
	p.current.posistion = 0
	p.current = p.current.next
	if p.current == nil {
		return ErrEndOfPlaylst
	}
	p.current.posistion = 0
	return nil
}

func (p *InMemoryPlayer) prevUnderLock() error {
	if p.current == nil {
		p.current = p.playlist
		if p.current == nil {
			return ErrPlaylistEmpty
		}
	}
	p.current.posistion = 0
	p.current = p.current.prev
	if p.current == nil {
		return ErrEndOfPlaylst
	}
	p.current.posistion = 0
	return nil
}

// run - should only run as a single instance
func (p *InMemoryPlayer) run(ctx context.Context) {
	t := time.NewTicker(tick)

	go func() {
		defer func() {
			select {
			case p.stopCh <- struct{}{}:
			default:
			}
		}()

		for {
			select {
			case <-ctx.Done():
				p.commands <- stop
			case <-t.C:
				p.handleTick()
			case cmd := <-p.commands:
				if mustStop := p.handleCommand(cmd, t, tick); mustStop {
					return
				}
			}
		}
	}()
}

func (p *InMemoryPlayer) handleTick() {
	p.mux.Lock()
	if p.state == PlayerStatePlaying {
		p.current.posistion += tick
		if p.current.posistion > p.current.song.Duration {
			p.commands <- next
		} else {
			for i := range p.callbacks {
				// prevent callback owner from modifying a song
				p.callbacks[i](p.current.posistion, *p.current.song)
			}
		}
	}

	p.mux.Unlock()
}

func (p *InMemoryPlayer) handleCommand(
	cmd command,
	t *time.Ticker,
	tick time.Duration,
) (mustStop bool) {
	p.mux.Lock()
	defer p.mux.Unlock()

	switch cmd {
	case start:
		p.initializePlayliastUnderLock()
		p.state = PlayerStatePlaying
	case pause:
		t.Stop()
		p.state = PlayerStatePaused
	case resume:
		t.Reset(tick)
		p.state = PlayerStatePlaying
	case stop:
		t.Stop()
		p.state = PlayerStateStopped
		return true
	case reset:
		t.Stop()
		p.resetPlaylistUnderLock()
	case next:
		t.Stop()
		p.state = PlayerStatePaused
		if err := p.nextUnderLock(); err != nil {
			p.state = PlayerStateStopped
			return true
		}
		t.Reset(tick)
		p.state = PlayerStatePlaying
	case prev:
		t.Stop()
		p.state = PlayerStatePaused
		if err := p.prevUnderLock(); err != nil {
			p.state = PlayerStateStopped
			return true
		}
		t.Reset(tick)
		p.state = PlayerStatePlaying
	}

	return false
}

func (p *InMemoryPlayer) initializePlayliastUnderLock() {
	p.current = p.playlist
	p.current.posistion = 0
}

func (p *InMemoryPlayer) resetPlaylistUnderLock() {
	p.current.posistion = 0
	p.current = nil
}
