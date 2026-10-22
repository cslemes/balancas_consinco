package service

import (
	"context"
	"log"
	"time"

	"balancas/internal/config"
	"github.com/kardianos/service"
)

// WindowsService implementa a interface service.Interface do pacote kardianos/service.
type WindowsService struct {
	cfg      *config.Config
	ctx      context.Context
	cancel   context.CancelFunc
	interval time.Duration
	syncFn   func(cfg *config.Config) (SyncStats, error)
}

// NewWindowsService inicializa uma nova instância de WindowsService.
func NewWindowsService(cfg *config.Config) *WindowsService {
	ctx, cancel := context.WithCancel(context.Background())
	var interval time.Duration
	if cfg != nil {
		interval = time.Duration(cfg.Sync.IntervalSeconds) * time.Second
	}
	if interval <= 0 {
		interval = 5 * time.Minute
	}

	return &WindowsService{
		cfg:      cfg,
		ctx:      ctx,
		cancel:   cancel,
		interval: interval,
		syncFn:   RunSyncCycle,
	}
}

// SetSyncFunc substitui a função executada a cada ciclo (útil para testes unitários).
func (s *WindowsService) SetSyncFunc(fn func(cfg *config.Config) (SyncStats, error)) {
	s.syncFn = fn
}

// SetInterval define um intervalo customizado para o loop do worker (útil para testes unitários).
func (s *WindowsService) SetInterval(d time.Duration) {
	s.interval = d
}

// Start inicia o worker loop em uma nova goroutine conforme a convenção do serviço Windows.
func (s *WindowsService) Start(svc service.Service) error {
	log.Println("Serviço Windows iniciando em background...")
	go s.run()
	return nil
}

// Stop finaliza a execução do worker loop cancelando o contexto do serviço.
func (s *WindowsService) Stop(svc service.Service) error {
	log.Println("Serviço Windows recebendo sinal de parada...")
	s.cancel()
	return nil
}

// run executa o primeiro ciclo imediatamente e em seguida inicia o loop baseado no intervalo configurado.
func (s *WindowsService) run() {
	interval := s.interval
	if interval <= 0 {
		if s.cfg != nil {
			interval = time.Duration(s.cfg.Sync.IntervalSeconds) * time.Second
		}
		if interval <= 0 {
			interval = 5 * time.Minute
		}
	}

	executeSync := s.syncFn
	if executeSync == nil {
		executeSync = RunSyncCycle
	}

	// Executa primeiro ciclo imediatamente
	_, _ = executeSync(s.cfg)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			log.Println("Worker encerrado com sucesso.")
			return
		case <-ticker.C:
			_, _ = executeSync(s.cfg)
		}
	}
}
