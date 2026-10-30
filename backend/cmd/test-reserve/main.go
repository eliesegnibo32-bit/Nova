// Command test-reserve validates the atomic stock reservation logic
// (cahier des charges ch. 4.3) against a live database.
//
// It runs the following sequence:
//  1. Connect to the DB (DATABASE_URL env var).
//  2. Resolve the test shop + variant (passed as CLI args or env vars).
//  3. Reset the variant's inventory to on_hand=5, reserved=0.
//  4. Reserve 3 → success (available = 2).
//  5. Reserve 3 again → FAIL (only 2 available) — must return
//     ErrInsufficientStock. This is the critical atomic check.
//  6. Release 3 → available = 5 again.
//  7. Exit 5 → on_hand = 0 (simulates successful delivery).
//  8. Reserve 1 → FAIL (out of stock).
//  9. Reset and print "ATOMIC RESERVATION OK" on success.
//
// Usage:
//
//      export DATABASE_URL='postgresql://...'
//      ./test-reserve -shop-id <uuid> -variant-id <uuid> -user-id <uuid>
//
// This is a development tool — it doesn't ship in the production binary.
package main

import (
        "context"
        "errors"
        "flag"
        "fmt"
        "log"
        "os"
        "time"

        "github.com/google/uuid"
        "github.com/jackc/pgx/v5/pgxpool"

        "nova-api/internal/repository"
)

func main() {
        shopID := flag.String("shop-id", "", "shop UUID (required)")
        variantID := flag.String("variant-id", "", "variant UUID (required)")
        userID := flag.String("user-id", "", "user UUID (the actor, required)")
        flag.Parse()

        if *shopID == "" || *variantID == "" || *userID == "" {
                fmt.Fprintln(os.Stderr, "Usage: test-reserve -shop-id <uuid> -variant-id <uuid> -user-id <uuid>")
                os.Exit(2)
        }
        sid, err := uuid.Parse(*shopID)
        if err != nil {
                log.Fatalf("invalid shop-id: %v", err)
        }
        vid, err := uuid.Parse(*variantID)
        if err != nil {
                log.Fatalf("invalid variant-id: %v", err)
        }
        uid, err := uuid.Parse(*userID)
        if err != nil {
                log.Fatalf("invalid user-id: %v", err)
        }

        dbURL := os.Getenv("DATABASE_URL")
        if dbURL == "" {
                log.Fatal("DATABASE_URL is not set")
        }

        ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
        defer cancel()

        pool, err := pgxpool.New(ctx, dbURL)
        if err != nil {
                log.Fatalf("connect db: %v", err)
        }
        defer pool.Close()

        repo := repository.NewInventoryRepository(pool)
        role := "owner"

        // Helper to print inventory state.
        printInv := func(label string) {
                inv, err := repo.GetByVariant(ctx, sid, uid, role, vid)
                if err != nil {
                        log.Fatalf("GetByVariant (%s): %v", label, err)
                }
                fmt.Printf("  [state] %-20s on_hand=%d reserved=%d available=%d\n", label, inv.OnHand, inv.Reserved, inv.Available)
        }

        fmt.Println("=== Atomic stock reservation test ===")
        fmt.Printf("shop_id=%s variant_id=%s user_id=%s\n", sid, vid, uid)
        printInv("initial")

        // Step 1: Reset to known state — set on_hand=5, reserved=0 via
        // a direct UPDATE bypassing the journal (this is a test tool,
        // not a real flow; production code ALWAYS goes through the
        // repository methods that create stock_movements).
        if err := resetInventory(ctx, pool, vid, sid, 5, 0); err != nil {
                log.Fatalf("reset inventory: %v", err)
        }
        printInv("after reset to on_hand=5")

        // Step 2: Reserve 3 → success (available = 2).
        fmt.Println("\n--- Step 2: Reserve 3 (should succeed) ---")
        inv, _, err := repo.Reserve(ctx, sid, uid, role, vid, repository.ReserveInput{
                Quantity: 3,
                AuthorID: &uid,
        })
        if err != nil {
                log.Fatalf("Reserve 3 (expected success): %v", err)
        }
        fmt.Printf("  OK: Reserve returned on_hand=%d reserved=%d available=%d\n", inv.OnHand, inv.Reserved, inv.Available)
        if inv.Available != 2 {
                log.Fatalf("FAIL: Expected available=2 after reserving 3, got %d", inv.Available)
        }

        // Step 3: Reserve 3 again → FAIL (only 2 available).
        fmt.Println("\n--- Step 3: Reserve 3 again (should FAIL with ErrInsufficientStock) ---")
        _, _, err = repo.Reserve(ctx, sid, uid, role, vid, repository.ReserveInput{
                Quantity: 3,
                AuthorID: &uid,
        })
        if err == nil {
                log.Fatalf("FAIL: Second Reserve 3 succeeded - expected ErrInsufficientStock (atomic check FAILED)")
        }
        if !errors.Is(err, repository.ErrInsufficientStock) {
                log.Fatalf("FAIL: Second Reserve returned %v - expected ErrInsufficientStock", err)
        }
        fmt.Printf("  OK: Reserve correctly returned ErrInsufficientStock: %v\n", err)
        printInv("after failed reserve")

        // Step 4: Release 3 → reserved = 0, available = 5.
        fmt.Println("\n--- Step 4: Release 3 (should succeed, available back to 5) ---")
        inv, _, err = repo.Release(ctx, sid, uid, role, vid, repository.ReleaseInput{
                Quantity: 3,
                AuthorID: &uid,
        })
        if err != nil {
                log.Fatalf("Release 3: %v", err)
        }
        fmt.Printf("  OK: Release returned on_hand=%d reserved=%d available=%d\n", inv.OnHand, inv.Reserved, inv.Available)
        if inv.Available != 5 || inv.Reserved != 0 {
                log.Fatalf("FAIL: Expected available=5 reserved=0 after release, got available=%d reserved=%d", inv.Available, inv.Reserved)
        }

        // Step 5: Reserve 5 then Exit 5 (simulates confirmed order → delivery).
        // ExitStock requires reserved >= quantity (it decrements both).
        fmt.Println("\n--- Step 5a: Reserve 5 (confirms order) ---")
        inv, _, err = repo.Reserve(ctx, sid, uid, role, vid, repository.ReserveInput{
                Quantity: 5,
                AuthorID: &uid,
        })
        if err != nil {
                log.Fatalf("Reserve 5 (before exit): %v", err)
        }
        fmt.Printf("  OK: Reserve returned on_hand=%d reserved=%d available=%d\n", inv.OnHand, inv.Reserved, inv.Available)
        if inv.Reserved != 5 || inv.Available != 0 {
                log.Fatalf("FAIL: Expected reserved=5 available=0, got reserved=%d available=%d", inv.Reserved, inv.Available)
        }

        fmt.Println("\n--- Step 5b: ExitStock 5 (delivery confirmed) ---")
        inv, _, err = repo.ExitStock(ctx, sid, uid, role, vid, repository.ExitStockInput{
                Quantity: 5,
                AuthorID: &uid,
        })
        if err != nil {
                log.Fatalf("ExitStock 5: %v", err)
        }
        fmt.Printf("  OK: ExitStock returned on_hand=%d reserved=%d available=%d\n", inv.OnHand, inv.Reserved, inv.Available)
        if inv.OnHand != 0 || inv.Reserved != 0 || inv.Available != 0 {
                log.Fatalf("FAIL: Expected on_hand=0 reserved=0 available=0 after exit, got on_hand=%d reserved=%d available=%d", inv.OnHand, inv.Reserved, inv.Available)
        }

        // Step 6: Try to reserve 1 → FAIL (out of stock).
        fmt.Println("\n--- Step 6: Reserve 1 (should FAIL - out of stock) ---")
        _, _, err = repo.Reserve(ctx, sid, uid, role, vid, repository.ReserveInput{
                Quantity: 1,
                AuthorID: &uid,
        })
        if !errors.Is(err, repository.ErrInsufficientStock) {
                log.Fatalf("FAIL: Reserve on empty stock returned %v - expected ErrInsufficientStock", err)
        }
        fmt.Printf("  OK: Reserve correctly returned ErrInsufficientStock on empty stock\n")

        // Step 7: Receive 3 (return goods from a refused delivery) then
        // ReturnStock 2 (simulates goods coming back after delivery).
        fmt.Println("\n--- Step 7: Receive 3 (re-approvisionnement) ---")
        inv, _, err = repo.ReceiveStock(ctx, sid, uid, role, vid, repository.ReceiveStockInput{
                Quantity: 3,
                Reason:   "re-appro test",
                AuthorID: uid,
        })
        if err != nil {
                log.Fatalf("ReceiveStock 3: %v", err)
        }
        if inv.OnHand != 3 || inv.Available != 3 {
                log.Fatalf("FAIL: Expected on_hand=3 available=3 after receive, got on_hand=%d available=%d", inv.OnHand, inv.Available)
        }
        fmt.Printf("  OK: ReceiveStock returned on_hand=%d reserved=%d available=%d\n", inv.OnHand, inv.Reserved, inv.Available)

        // Step 8: Negative adjustment (casse) — adjust -1 with reason.
        fmt.Println("\n--- Step 8: Adjust -1 (casse) ---")
        inv, _, err = repo.AdjustStock(ctx, sid, uid, role, vid, repository.AdjustStockInput{
                Delta:    -1,
                Reason:   "casse pendant transport",
                AuthorID: uid,
        })
        if err != nil {
                log.Fatalf("AdjustStock -1: %v", err)
        }
        if inv.OnHand != 2 || inv.Available != 2 {
                log.Fatalf("FAIL: Expected on_hand=2 available=2 after adjustment, got on_hand=%d available=%d", inv.OnHand, inv.Available)
        }
        fmt.Printf("  OK: AdjustStock returned on_hand=%d reserved=%d available=%d\n", inv.OnHand, inv.Reserved, inv.Available)

        // Reset for cleanliness — back to on_hand=5 so other tests can use the variant.
        if err := resetInventory(ctx, pool, vid, sid, 5, 0); err != nil {
                log.Fatalf("final reset: %v", err)
        }
        printInv("after final reset")

        fmt.Println("\n=== ATOMIC RESERVATION OK - all checks passed ===")
}

// resetInventory is a test-only helper that sets the inventory row to a
// known state. It uses a direct UPDATE bypassing the journal (which is
// fine for a test tool — production code ALWAYS goes through the
// repository methods that create stock_movements).
func resetInventory(ctx context.Context, pool *pgxpool.Pool, variantID, shopID uuid.UUID, onHand, reserved int) error {
        conn, err := pool.Acquire(ctx)
        if err != nil {
                return fmt.Errorf("acquire conn: %w", err)
        }
        defer conn.Release()
        // Set the session to super_admin to bypass RLS for the reset.
        if _, err := conn.Exec(ctx, "SELECT set_config('app.user_role', 'super_admin', true)"); err != nil {
                return err
        }
        if _, err := conn.Exec(ctx, "SELECT set_config('app.current_shop_id', $1, true)", shopID.String()); err != nil {
                return err
        }
        ct, err := conn.Exec(ctx, `
                UPDATE inventory SET on_hand = $2, reserved = $3, updated_at = now()
                 WHERE variant_id = $1
        `, variantID, onHand, reserved)
        if err != nil {
                return fmt.Errorf("update inventory: %w", err)
        }
        if ct.RowsAffected() == 0 {
                return fmt.Errorf("inventory row not found for variant %s", variantID)
        }
        return nil
}
