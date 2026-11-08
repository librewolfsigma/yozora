package handlers

import (
	"errors"
	"seanime/internal/api/anilist"
	"seanime/internal/customsource"

	"github.com/labstack/echo/v4"
)

// HandleCustomSourceListAnime
//
//	@summary returns a paginated list of anime from the provider.
//	@desc This will search for media from the provider.
//	@route /api/v1/custom-source/provider/list/anime [POST]
//	@returns hibikecustomsource.ListAnimeResponse
func (h *Handler) HandleCustomSourceListAnime(c echo.Context) error {

	type body struct {
		Provider string `json:"provider"`
		Search   string `json:"search"`
		Page     int    `json:"page"`
		PerPage  int    `json:"perPage"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	provider, ok := h.App.ExtensionRepository.GetCustomSourceExtensionByID(b.Provider)
	if !ok {
		return h.RespondWithError(c, errors.New("provider extension not found"))
	}

	res, err := provider.GetProvider().ListAnime(c.Request().Context(), b.Search, b.Page, b.PerPage)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	for i := range res.Media {
		customsource.NormalizeMedia(provider.GetExtensionIdentifier(), provider.GetID(), res.Media[i])
	}

	// YOZORA (fork): record the provider site URLs so entry metadata can be
	// recovered even when the extension's in-VM id registry is lost.
	h.rememberCustomSourceSiteUrls(provider.GetID(), res.Media)

	return h.RespondWithData(c, res)
}

// HandleCustomSourceListManga
//
//	@summary returns a paginated list of manga from the provider.
//	@desc This will search for media from the provider.
//	@route /api/v1/custom-source/provider/list/manga [POST]
//	@returns hibikecustomsource.ListMangaResponse
func (h *Handler) HandleCustomSourceListManga(c echo.Context) error {

	type body struct {
		Provider string `json:"provider"`
		Search   string `json:"search"`
		Page     int    `json:"page"`
		PerPage  int    `json:"perPage"`
	}

	var b body
	if err := c.Bind(&b); err != nil {
		return h.RespondWithError(c, err)
	}

	provider, ok := h.App.ExtensionRepository.GetCustomSourceExtensionByID(b.Provider)
	if !ok {
		return h.RespondWithError(c, errors.New("provider extension not found"))
	}

	res, err := provider.GetProvider().ListManga(c.Request().Context(), b.Search, b.Page, b.PerPage)
	if err != nil {
		return h.RespondWithError(c, err)
	}

	for i := range res.Media {
		customsource.NormalizeMedia(provider.GetExtensionIdentifier(), provider.GetID(), res.Media[i])
	}

	// YOZORA (fork): record the provider site URLs so entry metadata can be
	// recovered even when the extension's in-VM id registry is lost.
	h.rememberCustomSourceSiteUrlsManga(provider.GetID(), res.Media)

	return h.RespondWithData(c, res)
}

// customSourceManagerAccessor lets handlers reach the active platform's
// custom source manager without importing a concrete platform package.
type customSourceManagerAccessor interface {
	GetCustomSourceManager() *customsource.Manager
}

func (h *Handler) getCustomSourceManager() *customsource.Manager {
	if h.App == nil || h.App.AnilistPlatformRef == nil {
		return nil
	}
	if accessor, ok := h.App.AnilistPlatformRef.Get().(customSourceManagerAccessor); ok {
		return accessor.GetCustomSourceManager()
	}
	return nil
}

// rememberCustomSourceSiteUrls persists localId -> siteUrl for the returned media.
func (h *Handler) rememberCustomSourceSiteUrls(extId string, media []*anilist.BaseAnime) {
	mgr := h.getCustomSourceManager()
	if mgr == nil || len(media) == 0 {
		return
	}
	pairs := make(map[int]string)
	for _, m := range media {
		if m == nil || m.SiteURL == nil || *m.SiteURL == "" {
			continue
		}
		rawUrl, ok := customsource.StripCustomSourceSiteUrlTag(*m.SiteURL)
		if !ok {
			continue
		}
		_, localId := customsource.ExtractExtensionData(m.ID)
		pairs[localId] = rawUrl
	}
	mgr.RememberSiteUrls(extId, pairs)
}

// rememberCustomSourceSiteUrlsManga persists localId -> siteUrl for the returned manga.
func (h *Handler) rememberCustomSourceSiteUrlsManga(extId string, media []*anilist.BaseManga) {
	mgr := h.getCustomSourceManager()
	if mgr == nil || len(media) == 0 {
		return
	}
	pairs := make(map[int]string)
	for _, m := range media {
		if m == nil || m.SiteURL == nil || *m.SiteURL == "" {
			continue
		}
		rawUrl, ok := customsource.StripCustomSourceSiteUrlTag(*m.SiteURL)
		if !ok {
			continue
		}
		_, localId := customsource.ExtractExtensionData(m.ID)
		pairs[localId] = rawUrl
	}
	mgr.RememberSiteUrls(extId, pairs)
}
