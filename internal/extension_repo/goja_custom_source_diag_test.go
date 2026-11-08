package extension_repo

import (
        "context"
        "os"
        "testing"

        "seanime/internal/extension"
        "seanime/internal/goja/goja_runtime"

        "github.com/goccy/go-json"
        "github.com/rs/zerolog"
)

// Diagnostic test: loads the real HMVmania custom source payload and checks
// that getAnime can resolve a media ID that was returned by listAnime in a
// fresh VM (the sandbox server logs showed this path returning zero results).
func TestHmvmaniaCsGetAnimeDiagnostic(t *testing.T) {
        if testing.Short() {
                t.Skip("skipping live network test")
        }

        payloadPath := "/home/z/my-project/.yozora/data/extensions/yozora-hmvmania-cs.json"
        if _, err := os.Stat(payloadPath); err != nil {
                t.Skip("payload not present")
        }
        raw, err := os.ReadFile(payloadPath)
        if err != nil {
                t.Fatal(err)
        }
        var manifest struct {
                ID      string `json:"id"`
                Payload string `json:"payload"`
        }
        if err := json.Unmarshal(raw, &manifest); err != nil {
                t.Fatal(err)
        }

        logger := zerolog.New(os.Stdout).Level(zerolog.TraceLevel)
        runtimeManager := goja_runtime.NewManager(&logger)

        ext := &extension.Extension{
                ID:          manifest.ID,
                Name:        manifest.ID,
                Version:     "1.0.0",
                ManifestURI: "builtin",
                Language:    extension.LanguageTypescript,
                Type:        extension.TypeCustomSource,
                Payload:     manifest.Payload,
        }

        provider, _, err := NewGojaCustomSource(ext, extension.LanguageTypescript, &logger, runtimeManager, nil)
        if err != nil {
                t.Fatalf("failed to create provider: %v", err)
        }
        cse := extension.NewCustomSourceExtension(ext, provider)
        cse.SetExtensionIdentifier(339)

        // 1. listAnime (page 1) — like the browse page
        listRes, err := provider.ListAnime(context.Background(), "", 1, 10)
        if err != nil {
                t.Fatalf("listAnime failed: %v", err)
        }
        if len(listRes.Media) == 0 {
                t.Fatal("listAnime returned no media")
        }
        localId := listRes.Media[0].ID
        t.Logf("listAnime OK: id=%d title=%s", localId, *listRes.Media[0].GetTitle().GetUserPreferred())

        // 2. getAnime with the local id, but through a fresh pool VM
        animes, err := provider.GetAnime(context.Background(), []int{localId})
        if err != nil {
                t.Fatalf("getAnime failed: %v", err)
        }
        t.Logf("getAnime returned %d results", len(animes))
        if len(animes) == 0 {
                t.Fatal("getAnime returned ZERO results for an id that listAnime just returned (registry not healed)")
        }

        // 3. getAnimeDetails
        details, err := provider.GetAnimeDetails(context.Background(), localId)
        if err != nil {
                t.Fatalf("getAnimeDetails failed: %v", err)
        }
        if details == nil {
                t.Fatal("getAnimeDetails returned nil")
        }
        t.Logf("getAnimeDetails OK: id=%d", details.ID)

        // 4. Fresh runtime manager (simulates server restart) then getAnime
        runtimeManager2 := goja_runtime.NewManager(&logger)
        provider2, _, err := NewGojaCustomSource(ext, extension.LanguageTypescript, &logger, runtimeManager2, nil)
        if err != nil {
                t.Fatalf("failed to create provider2: %v", err)
        }
        animes2, err := provider2.GetAnime(context.Background(), []int{localId})
        if err != nil {
                t.Fatalf("getAnime after restart failed: %v", err)
        }
        if len(animes2) == 0 {
                t.Fatal("getAnime after restart returned ZERO results (healRegistry failed)")
        }
        t.Logf("getAnime after restart OK: %d results", len(animes2))
        _ = cse
}
