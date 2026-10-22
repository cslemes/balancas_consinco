package service

import (
	"context"
	"log"
	"sync"
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
	wg       sync.WaitGroup
	syncFn   func(ctx context.Context, cfg *config.Config) (SyncStats, error)
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
func (s *WindowsService) SetSyncFunc(fn func(ctx context.Context, cfg *config.Config) (SyncStats, error)) {
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

// Stop finaliza a execução do worker loop cancelando o contexto do serviço e aguardando encerramento.
func (s *WindowsService) Stop(svc service.Service) error {
	log.Println("Serviço Windows recebendo sinal de parada...")
	s.cancel()

	done := make(chan struct{})
	go func() {
		s.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
		log.Println("Serviço Windows finalizado com sucesso.")
	case <-time.After(15 * time.Second):
		log.Println("[AVISO] Timeout de 15s aguardando finalização das rotinas do serviço.")
	}

	return nil
}

// run executa o primeiro ciclo imediatamente e em seguida inicia o loop baseado no intervalo configurado.
func (s *WindowsService) run() {
	s.wg.Add(1)
	defer s.wg.Done()

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

	// Executa primeiro ciclo imediatamente se não cancelado
	if s.ctx.Err() == nil {
		if _, err := executeSync(s.ctx, s.cfg); err != nil && err != context.Canceled {
			log.Printf("[ERRO] Falha no ciclo de sincronização: %v", err)
		}
	}

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-s.ctx.Done():
			log.Println("Worker encerrado com sucesso.")
			return
		case <-ticker.C:
			if _, err := executeSync(s.ctx, s.cfg); err != nil && err != context.Canceled {
				log.Printf("[ERRO] Falha no ciclo de sincronização: %v", err)
			}
		}
	}
}
