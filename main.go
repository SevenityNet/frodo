package main

import (
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"math/big"
	"os"
	"strconv"
	"time"

	badger "github.com/dgraph-io/badger/v4"
	"github.com/gofiber/fiber/v3"
	_ "github.com/joho/godotenv/autoload"
)

func main() {
	opts := badger.DefaultOptions("badger")
	opts.IndexCacheSize = 100 * 1024 * 1024

	db, err := badger.Open(opts)
	if err != nil {
		panic(err)
	}

	defer db.Close()

	kv := &KV{db: db}
	app := fiber.New()
	apiKey := os.Getenv("API_KEY")
	shortCode, err := strconv.Atoi(os.Getenv("SHORT_CODE_LENGTH"))
	if err != nil {
		shortCode = 6
		log.Println("error parsing SHORT_CODE_LENGTH, defaulting to 6")
	}

	app.Get("/:shortcode", func(c fiber.Ctx) error {
		return redirectHandler(c, kv)
	})

	app.Post("/api/shorten", func(c fiber.Ctx) error {
		if c.Get("X-API-KEY") != apiKey {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "wrong api key",
			})
		}

		return createHandler(c, kv, shortCode)
	})

	app.Delete("/api/shorten/:shortcode", func(c fiber.Ctx) error {
		if c.Get("X-API-KEY") != apiKey {
			return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
				"error": "wrong api key",
			})
		}

		return deleteHandler(c, kv)
	})

	log.Fatal(app.Listen(":" + os.Getenv("PORT")))
}

func redirectHandler(c fiber.Ctx, db *KV) error {
	shortCode := c.Params("shortcode")

	exists, err := db.Exists(shortCode)
	if err != nil {
		log.Println("badger error checking if key exists:", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	if !exists {
		return c.SendStatus(fiber.StatusNotFound)
	}

	url, err := db.Get(shortCode)
	if err != nil {
		log.Println("badger error getting value:", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Redirect().To(url)
}

func createHandler(c fiber.Ctx, db *KV, shortCodeLength int) error {
	shortCode := c.FormValue("custom", "")
	url := c.FormValue("url", "")
	expiry := c.FormValue("expiry", "")
	expiryMinutes := 0

	if url == "" {
		return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
			"error": "url is required",
		})
	}

	if expiry != "" {
		exp, err := strconv.Atoi(expiry)
		if err != nil {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "expiry must be a number",
			})
		}

		if exp < 1 {
			return c.Status(fiber.StatusBadRequest).JSON(fiber.Map{
				"error": "expiry must be greater than 0",
			})
		}

		expiryMinutes = exp
	}

	if shortCode == "" {
		code, err := GenerateRandomString(shortCodeLength)
		if err != nil {
			log.Println("error generating random string:", err)
			return c.SendStatus(fiber.StatusInternalServerError)
		}

		shortCode = code
	}

	if err := db.Set(shortCode, url, expiryMinutes); err != nil {
		log.Println("badger error setting key:", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.Status(fiber.StatusCreated).JSON(fiber.Map{
		"short_code": shortCode,
	})
}

func deleteHandler(c fiber.Ctx, db *KV) error {
	shortCode := c.Params("shortcode")

	exists, err := db.Exists(shortCode)
	if err != nil {
		log.Println("badger error checking if key exists:", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	if !exists {
		return c.SendStatus(fiber.StatusNotFound)
	}

	if err := db.Delete(shortCode); err != nil {
		log.Println("badger error deleting key:", err)
		return c.SendStatus(fiber.StatusInternalServerError)
	}

	return c.SendStatus(fiber.StatusNoContent)
}

type KV struct {
	db *badger.DB
}

func (k *KV) Exists(key string) (bool, error) {
	var exists bool
	err := k.db.View(
		func(tx *badger.Txn) error {
			if val, err := tx.Get([]byte(key)); err != nil {
				return err
			} else if val != nil {
				exists = true
			}
			return nil
		})
	if errors.Is(err, badger.ErrKeyNotFound) {
		err = nil
	}
	return exists, err
}

func (k *KV) Get(key string) (string, error) {
	var value string
	return value, k.db.View(
		func(tx *badger.Txn) error {
			item, err := tx.Get([]byte(key))
			if err != nil {
				return fmt.Errorf("getting value: %w", err)
			}
			valCopy, err := item.ValueCopy(nil)
			if err != nil {
				return fmt.Errorf("copying value: %w", err)
			}
			value = string(valCopy)
			return nil
		})
}

func (k *KV) Set(key, value string, expiryMinutes int) error {
	return k.db.Update(
		func(txn *badger.Txn) error {
			if expiryMinutes == 0 {
				return txn.Set([]byte(key), []byte(value))
			}

			entry := badger.NewEntry([]byte(key), []byte(value))
			entry.WithTTL(time.Duration(expiryMinutes) * time.Minute)

			return txn.SetEntry(entry)
		})
}

func (k *KV) Delete(key string) error {
	return k.db.Update(
		func(txn *badger.Txn) error {
			return txn.Delete([]byte(key))
		})
}

func GenerateRandomString(n int) (string, error) {
	const letters = "0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"
	ret := make([]byte, n)
	for i := 0; i < n; i++ {
		num, err := rand.Int(rand.Reader, big.NewInt(int64(len(letters))))
		if err != nil {
			return "", err
		}
		ret[i] = letters[num.Int64()]
	}

	return string(ret), nil
}
