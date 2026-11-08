package shared_platform

import (
        "context"
        "errors"
        "testing"

        "seanime/internal/api/anilist"
        "seanime/internal/api/metadata"
        "seanime/internal/customsource"
        "seanime/internal/extension"
        hibikecustomsource "seanime/internal/extension/hibike/customsource"
        "seanime/internal/testutil"
        "seanime/internal/util"

        "github.com/stretchr/testify/require"
)

// stubCustomSourceProvider implements hibikecustomsource.Provider with
// configurable manga/anime behaviour for the recovery-path tests below.
type stubCustomSourceProvider struct {
        mangaCalls int
        animeCalls int
        mangaFails int // number of initial GetManga calls that fail
        animeFails int // number of initial GetAnime calls that fail
        mangaMedia *anilist.BaseManga
        animeMedia *anilist.BaseAnime
}

func (p *stubCustomSourceProvider) GetExtensionIdentifier() int { return 738 }

func (p *stubCustomSourceProvider) GetSettings() hibikecustomsource.Settings {
        return hibikecustomsource.Settings{SupportsAnime: true, SupportsManga: true}
}

func (p *stubCustomSourceProvider) GetAnime(ctx context.Context, ids []int) ([]*anilist.BaseAnime, error) {
        p.animeCalls++
        if p.animeCalls <= p.animeFails {
                return nil, errors.New("upstream blip")
        }
        if p.animeMedia == nil {
                return nil, nil
        }
        return []*anilist.BaseAnime{p.animeMedia}, nil
}

func (p *stubCustomSourceProvider) ListAnime(ctx context.Context, search string, page int, perPage int) (*hibikecustomsource.ListAnimeResponse, error) {
        return nil, errors.New("not implemented in stub")
}

func (p *stubCustomSourceProvider) GetAnimeWithRelations(ctx context.Context, id int) (*anilist.CompleteAnime, error) {
        return nil, errors.New("not implemented in stub")
}

func (p *stubCustomSourceProvider) GetAnimeMetadata(ctx context.Context, id int) (*metadata.AnimeMetadata, error) {
        return nil, errors.New("not implemented in stub")
}

func (p *stubCustomSourceProvider) GetAnimeDetails(ctx context.Context, id int) (*anilist.AnimeDetailsById_Media, error) {
        return nil, errors.New("not implemented in stub")
}

func (p *stubCustomSourceProvider) GetManga(ctx context.Context, ids []int) ([]*anilist.BaseManga, error) {
        p.mangaCalls++
        if p.mangaCalls <= p.mangaFails {
                return nil, errors.New("upstream blip")
        }
        if p.mangaMedia == nil {
                return nil, nil
        }
        return []*anilist.BaseManga{p.mangaMedia}, nil
}

func (p *stubCustomSourceProvider) ListManga(ctx context.Context, search string, page int, perPage int) (*hibikecustomsource.ListMangaResponse, error) {
        return nil, errors.New("not implemented in stub")
}

func (p *stubCustomSourceProvider) GetMangaDetails(ctx context.Context, id int) (*anilist.MangaDetailsById_Media, error) {
        return nil, errors.New("not implemented in stub")
}

func newCustomSourceTestHelper(t *testing.T, provider hibikecustomsource.Provider) (*PlatformHelper, *customsource.Manager) {
        t.Helper()

        env := testutil.NewTestEnv(t)
        logger := env.Logger()
        database := env.MustNewDatabase(logger)

        bank := extension.NewUnifiedBank()
        ext := extension.NewCustomSourceExtension(&extension.Extension{
                ID:   "test-cs",
                Name: "Test CS",
                Type: extension.TypeCustomSource,
        }, provider)
        ext.SetExtensionIdentifier(738)
        bank.Set("test-cs", ext)

        bankRef := util.NewRef(bank)
        helper := NewPlatformHelper(bankRef, database, logger)
        manager := helper.GetCustomSourceManager()
        require.NotNil(t, manager)
        return helper, manager
}

// After a transient provider failure the handler retries the provider; when
// the retry succeeds the REAL media is returned (provisional=false) so
// callers can cache it.
func TestHandleCustomSourceMangaRetriesTransientFailure(t *testing.T) {
        provider := &stubCustomSourceProvider{
                mangaFails: 1,
                mangaMedia: &anilist.BaseManga{
                        ID:      695267,
                        SiteURL: new("https://nhentai.net/g/695267/"),
                        Title:   &anilist.BaseManga_Title{English: new("Real Gallery Title")},
                },
        }
        helper, _ := newCustomSourceTestHelper(t, provider)

        mediaID := customsource.GenerateMediaId(738, 695267)
        media, isCustom, provisional, err := helper.HandleCustomSourceManga(context.Background(), mediaID)

        require.NoError(t, err)
        require.True(t, isCustom)
        require.False(t, provisional, "retry healed the transient failure; media is real and cacheable")
        require.Equal(t, 2, provider.mangaCalls, "exactly one retry after the transient failure")
        require.NotNil(t, media)
        require.NotNil(t, media.GetTitle().GetEnglish())
        require.Equal(t, "Real Gallery Title", *media.GetTitle().GetEnglish())
        require.Equal(t, mediaID, media.ID, "normalized to the composite custom-source id")
}

// When the provider keeps failing, the handler synthesizes a provisional
// recovery placeholder from the remembered site URL. Callers must NOT cache
// provisional media — the flag exists so a later request can retry the
// provider and replace the placeholder with real metadata.
func TestHandleCustomSourceMangaPersistentFailureReturnsProvisional(t *testing.T) {
        provider := &stubCustomSourceProvider{
                mangaFails: 99,
        }
        helper, manager := newCustomSourceTestHelper(t, provider)

        localId := 695267
        manager.RememberSiteUrls("test-cs", map[int]string{
                localId: "https://nhentai.net/g/695267/",
        })

        mediaID := customsource.GenerateMediaId(738, localId)
        media, isCustom, provisional, err := helper.HandleCustomSourceManga(context.Background(), mediaID)

        require.NoError(t, err)
        require.True(t, isCustom)
        require.True(t, provisional, "placeholder from remembered site URL must be marked provisional (do not cache)")
        require.NotNil(t, media)
        require.NotNil(t, media.GetTitle().GetEnglish())
        require.Contains(t, *media.GetTitle().GetEnglish(), "695267", "slug-derived title from the remembered URL")
        require.Equal(t, 2, provider.mangaCalls, "initial attempt + single retry, then recovery")

        // Self-healing contract: once the upstream answers again, a fresh call
        // returns real metadata (this is only possible because provisional media
        // is never cached).
        provider.mangaFails = 0
        provider.mangaMedia = &anilist.BaseManga{
                ID:      localId,
                SiteURL: new("https://nhentai.net/g/695267/"),
                Title:   &anilist.BaseManga_Title{English: new("Real Gallery Title")},
        }
        media2, _, provisional2, err2 := helper.HandleCustomSourceManga(context.Background(), mediaID)
        require.NoError(t, err2)
        require.False(t, provisional2)
        require.NotNil(t, media2.GetTitle().GetEnglish())
        require.Equal(t, "Real Gallery Title", *media2.GetTitle().GetEnglish())
}

// Without a remembered site URL there is nothing to recover from — the
// handler must surface an honest error instead of a fake entry.
func TestHandleCustomSourceMangaUnresolvableIdErrors(t *testing.T) {
        provider := &stubCustomSourceProvider{
                mangaFails: 99,
        }
        helper, _ := newCustomSourceTestHelper(t, provider)

        mediaID := customsource.GenerateMediaId(738, 111222)
        _, _, _, err := helper.HandleCustomSourceManga(context.Background(), mediaID)

        // The underlying provider error is surfaced (more informative than a
        // generic message) because there is nothing to recover with.
        require.Error(t, err)
        require.Contains(t, err.Error(), "upstream blip")
}

// Anime mirrors the manga behaviour: transient failure retried once, then
// real media returned with provisional=false.
func TestHandleCustomSourceAnimeRetriesTransientFailure(t *testing.T) {
        provider := &stubCustomSourceProvider{
                animeFails: 1,
                animeMedia: &anilist.BaseAnime{
                        ID:      4083405537 % (1 << 40),
                        SiteURL: new("https://hmvmania.com/video/some-hmv/"),
                        Title:   &anilist.BaseAnime_Title{English: new("Real HMV Title")},
                },
        }
        helper, _ := newCustomSourceTestHelper(t, provider)

        mediaID := customsource.GenerateMediaId(738, 4083405537%(1<<40))
        media, isCustom, provisional, err := helper.HandleCustomSourceAnime(context.Background(), mediaID)

        require.NoError(t, err)
        require.True(t, isCustom)
        require.False(t, provisional)
        require.Equal(t, 2, provider.animeCalls)
        require.NotNil(t, media)
        require.NotNil(t, media.GetTitle().GetEnglish())
        require.Equal(t, "Real HMV Title", *media.GetTitle().GetEnglish())
}
