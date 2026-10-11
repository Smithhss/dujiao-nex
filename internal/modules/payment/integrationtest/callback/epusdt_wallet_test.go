package paymentcallback_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/dujiao-next/internal/constants"
	paymentapp "github.com/dujiao-next/internal/modules/payment/application"
	paymentdomain "github.com/dujiao-next/internal/modules/payment/domain"
	epusdtadapter "github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/adapters/epusdt"
	"github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/epusdt"
	paymentprovider "github.com/dujiao-next/internal/modules/payment/infrastructure/gateway/provider"
	paymentstore "github.com/dujiao-next/internal/modules/payment/infrastructure/gormstore"
	paymentcallback "github.com/dujiao-next/internal/modules/payment/transport/http/callback"
	walletapp "github.com/dujiao-next/internal/modules/wallet/application"
	walletdomain "github.com/dujiao-next/internal/modules/wallet/domain"
	walletstore "github.com/dujiao-next/internal/modules/wallet/infrastructure/gormstore"
	"github.com/dujiao-next/internal/shared/jsonmap"
	"github.com/dujiao-next/internal/shared/money"
	"github.com/gin-gonic/gin"
	"github.com/shopspring/decimal"
)

// All simulated callbacks stay in a fresh in-memory database and use a test key.
func TestEpusdtWalletCallbackOffline(t *testing.T) {
	for _, scenario := range []string{"success_and_duplicate", "bad_signature", "tampered_amount", "signed_wrong_amount", "wrong_order", "wrong_currency", "non_success"} {
		t.Run(scenario, func(t *testing.T) {
			f := newOkpayCallbackFixture(t)
			if err := f.db.AutoMigrate(&walletdomain.Account{}, &walletdomain.Transaction{}, &walletdomain.RechargeOrder{}); err != nil {
				t.Fatal(err)
			}
			amount := money.FromDecimal(decimal.RequireFromString("0.68"))
			cfg := jsonmap.JSON{"gateway_url": "https://example.invalid", "pid": "1000", "secret_key": "offline-test-key", "currency": "cny", "token": "usdt", "network": "tron"}
			if scenario == "wrong_currency" {
				cfg["currency"] = "usd"
			}
			channel := &paymentdomain.PaymentChannel{ID: f.payment.ChannelID, Name: "offline-gmpay", ProviderType: constants.PaymentProviderEpusdt, ChannelType: "epusdt", InteractionMode: constants.PaymentInteractionRedirect, ConfigJSON: cfg, IsActive: false}
			if err := f.db.Save(channel).Error; err != nil {
				t.Fatal(err)
			}
			f.payment.OrderID = 0
			f.payment.ProviderType = constants.PaymentProviderEpusdt
			f.payment.ChannelType = "epusdt"
			f.payment.Amount = amount
			f.payment.Currency = "CNY"
			f.payment.ProviderRef = "offline-gmpay-trade"
			if err := f.db.Save(f.payment).Error; err != nil {
				t.Fatal(err)
			}
			recharge := &walletdomain.RechargeOrder{RechargeNo: "OFFLINE-RECHARGE", UserID: f.order.UserID, PaymentID: f.payment.ID, ChannelID: channel.ID, ProviderType: channel.ProviderType, ChannelType: channel.ChannelType, InteractionMode: channel.InteractionMode, Amount: amount, PayableAmount: amount, Currency: "CNY", Status: constants.WalletRechargeStatusPending}
			if err := f.db.Create(recharge).Error; err != nil {
				t.Fatal(err)
			}
			account := &walletdomain.Account{UserID: f.order.UserID, Balance: money.FromDecimal(decimal.Zero)}
			if err := f.db.Create(account).Error; err != nil {
				t.Fatal(err)
			}
			channels := paymentstore.NewChannelStore(f.db)
			registry := paymentprovider.NewRegistry()
			registry.Register(constants.PaymentProviderEpusdt, "", epusdtadapter.NewEpusdtAdapter())
			walletRepo := walletstore.New(f.db)
			walletSvc := walletapp.NewService(walletapp.Options{Repository: walletRepo, Transactions: walletRepo})
			svc := paymentapp.NewPaymentService(paymentapp.PaymentServiceOptions{PaymentStore: f.paymentRepo, ChannelStore: channels, WalletRepo: walletRepo, WalletService: walletSvc, PaymentProviderRegistry: registry})
			handler := paymentcallback.NewHandler(callbackServiceTestAdapter{payments: svc}, f.paymentRepo, channels, nil)
			params := map[string]interface{}{"pid": "1000", "trade_id": f.payment.ProviderRef, "order_id": f.payment.GatewayOrderNo, "amount": 0.68, "actual_amount": 0.1, "receive_address": "offline-address", "token": "USDT", "block_transaction_id": "offline-tx", "status": 2}
			switch scenario {
			case "signed_wrong_amount":
				params["amount"] = 0.67
			case "wrong_order":
				params["order_id"] = "wrong-order"
			case "non_success":
				params["status"] = 1
			}
			params["signature"] = epusdt.Sign(params, "offline-test-key")
			if scenario == "bad_signature" {
				params["signature"] = "invalid"
			}
			if scenario == "tampered_amount" {
				params["amount"] = 0.67
			}
			body, err := json.Marshal(params)
			if err != nil {
				t.Fatal(err)
			}
			attempts := 1
			if scenario == "success_and_duplicate" {
				attempts = 2
			}
			for i := 0; i < attempts; i++ {
				recorder := httptest.NewRecorder()
				ctx, _ := gin.CreateTestContext(recorder)
				ctx.Request = httptest.NewRequest(http.MethodPost, "/api/v1/payments/callback", bytes.NewReader(body))
				ctx.Request.Header.Set("Content-Type", "application/json")
				handler.PaymentCallback(ctx)
				want := constants.EpusdtCallbackFail
				if scenario == "success_and_duplicate" {
					want = constants.EpusdtCallbackSuccess
				}
				if recorder.Body.String() != want {
					t.Fatalf("callback attempt %d: got %q, want %q", i, recorder.Body.String(), want)
				}
			}
			if err := f.db.First(account, account.ID).Error; err != nil {
				t.Fatal(err)
			}
			var ledgerCount int64
			if err := f.db.Model(&walletdomain.Transaction{}).Count(&ledgerCount).Error; err != nil {
				t.Fatal(err)
			}
			wantBalance, wantCount := decimal.Zero, int64(0)
			wantStatus := constants.PaymentStatusPending
			if scenario == "success_and_duplicate" {
				wantBalance, wantCount, wantStatus = amount.Decimal, 1, constants.PaymentStatusSuccess
			}
			stored, err := f.paymentRepo.GetByID(f.payment.ID)
			if err != nil || stored == nil {
				t.Fatalf("load payment: %v", err)
			}
			if !account.Balance.Decimal.Equal(wantBalance) || ledgerCount != wantCount || stored.Status != wantStatus {
				t.Fatalf("balance=%s ledger=%d status=%s; want %s/%d/%s", account.Balance.String(), ledgerCount, stored.Status, wantBalance, wantCount, wantStatus)
			}
		})
	}
}
