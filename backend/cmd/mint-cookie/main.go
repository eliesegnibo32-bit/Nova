// Command mint-cookie signs a fake dev session cookie for manual testing.
//
// Usage:
//
//      SESSION_SECRET=... go run ./cmd/mint-cookie -uid <uuid> -role super_admin
package main

import (
        "flag"
        "fmt"
        "os"
        "time"

        "github.com/google/uuid"
        "nova-api/internal/auth"
)

func main() {
        uidStr := flag.String("uid", "11111111-1111-1111-1111-111111111111", "user UUID")
        role := flag.String("role", "super_admin", "session role")
        shopID := flag.String("shop-id", "", "shop UUID (optional)")
        ttl := flag.Duration("ttl", 24*time.Hour, "session TTL")
        flag.Parse()

        secret := []byte(os.Getenv("SESSION_SECRET"))
        if len(secret) < 32 {
                fmt.Fprintln(os.Stderr, "SESSION_SECRET must be >= 32 bytes")
                os.Exit(1)
        }
        uid, err := uuid.Parse(*uidStr)
        if err != nil {
                fmt.Fprintln(os.Stderr, "parse uid:", err)
                os.Exit(1)
        }
        sess := auth.Session{
                UserID:    uid,
                Role:      *role,
                ExpiresAt: time.Now().Add(*ttl),
        }
        if *shopID != "" {
                sid, err := uuid.Parse(*shopID)
                if err != nil {
                        fmt.Fprintln(os.Stderr, "parse shop_id:", err)
                        os.Exit(1)
                }
                sess.ShopID = &sid
        }
        cookie, err := auth.SignSession(sess, secret)
        if err != nil {
                fmt.Fprintln(os.Stderr, "sign:", err)
                os.Exit(1)
        }
        fmt.Print(cookie)
}
