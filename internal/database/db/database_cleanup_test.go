package db

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"seanime/internal/database/models"
	"seanime/internal/util"
	"testing"
	"time"

	"github.com/dustin/go-humanize"
)

// YOZORA (fork): the original data budget writes ~5 GB of SQLite data
// (10 000 × 500 KB local-file rows, etc.), which exceeds sandbox/CI disks and
// can trip disk-pressure reapers. The trimming behavior under test is
// row-count based, so the budget is configurable via SEANIME_TEST_CLEANUP_SCALE
// (a fraction of the original row counts AND row sizes, 0 < s <= 1; the derived
// expectations scale with the counts). Default is 1 — identical to upstream.
const cleanupScaleEnv = "SEANIME_TEST_CLEANUP_SCALE"

type cleanupBudget struct {
	scanSummaryRows int
	scanSummarySize int
	localFileRows   int
	localFileSize   int
	historyRows     int
	historySize     int
}

func cleanupBudgetForScale(scale float64) cleanupBudget {
	if scale <= 0 || scale > 1 {
		scale = 1
	}
	scaleInt := func(n int) int {
		v := int(float64(n) * scale)
		if v < n && v < 1 {
			v = 1
		}
		return v
	}
	scaleSize := func(n int) int {
		v := int(float64(n) * scale)
		// Keep sizes above the 4 KB SQLite page so the data stays representative.
		if v < 4096 {
			v = 4096
		}
		if v > n {
			v = n
		}
		return v
	}
	return cleanupBudget{
		scanSummaryRows: scaleInt(10000),
		scanSummarySize: scaleSize(50000),
		localFileRows:   scaleInt(10000),
		localFileSize:   scaleSize(500000),
		historyRows:     scaleInt(1000),
		historySize:     scaleSize(50000),
	}
}

func TestDatabaseCleanupManager(t *testing.T) {
	scale := 1.0
	if raw := os.Getenv(cleanupScaleEnv); raw != "" {
		parsed, err := strconv.ParseFloat(raw, 64)
		if err == nil && parsed > 0 && parsed <= 1 {
			scale = parsed
		}
	}
	budget := cleanupBudgetForScale(scale)
	t.Logf("cleanup budget (scale=%.2f): scanSummary=%dx%d B, localFiles=%dx%d B, history=%dx%d B",
		scale, budget.scanSummaryRows, budget.scanSummarySize, budget.localFileRows, budget.localFileSize, budget.historyRows, budget.historySize)

	tempDir := t.TempDir()
	logger := util.NewLogger()

	database, err := NewDatabase(tempDir, "cleanup_test", logger)
	if err != nil {
		t.Fatalf("Failed to create test database: %v", err)
	}

	t.Log("Populating database with test data...")
	populateCleanupTestData(t, database, budget)

	checkTableCounts(t, database, "before cleanup", budget)

	dbPath := filepath.Join(tempDir, "cleanup_test.db")
	if stat, err := os.Stat(dbPath); err != nil {
		t.Errorf("Database file does not exist: %v", err)
	} else {
		t.Logf("Database file size: %s bytes", humanize.Bytes(uint64(stat.Size())))
	}

	t.Log("Running database cleanup...")
	database.RunDatabaseCleanup()

	// DEVNOTE: Locking issues occur when running this in parallel to many writes
	//go func() {
	//database.TrimLocalFileEntries()
	//database.TrimTorrentstreamHistory()
	//database.TrimScanSummaryEntries()
	//}()

	t.Log("Launching many write operations...")
	time.Sleep(100 * time.Millisecond)
	for i := 0; i < 1000; i++ {
		go database.Gorm().Create(&models.ScanSummary{Value: []byte(fmt.Sprintf("scan summary data %d - %s", i, generateCleanupTestData(budget.scanSummarySize)))})
	}

	time.Sleep(20 * time.Second)

	checkTableCounts(t, database, "after cleanup", budget)

	if stat, err := os.Stat(dbPath); err != nil {
		t.Errorf("Database file does not exist: %v", err)
	} else {
		t.Logf("Database file size: %s bytes", humanize.Bytes(uint64(stat.Size())))
	}

	t.Log("Cleanup manager test completed successfully")
}

func populateCleanupTestData(t *testing.T, database *Database, budget cleanupBudget) {
	for i := 0; i < budget.scanSummaryRows; i++ {
		scanSummary := &models.ScanSummary{
			Value: []byte(fmt.Sprintf("scan summary data %d - %s", i, generateCleanupTestData(budget.scanSummarySize))),
		}
		err := database.Gorm().Create(scanSummary).Error
		if err != nil {
			t.Fatalf("Failed to create scan summary: %v", err)
		}
	}

	for i := 0; i < budget.localFileRows; i++ {
		localFiles := &models.LocalFiles{
			Value: []byte(fmt.Sprintf("local files data %d - %s", i, generateCleanupTestData(budget.localFileSize))),
		}
		err := database.Gorm().Create(localFiles).Error
		if err != nil {
			t.Fatalf("Failed to create local files: %v", err)
		}
	}

	for i := 0; i < budget.historyRows; i++ {
		history := &models.TorrentstreamHistory{
			MediaId: i % 10,
			Torrent: []byte(fmt.Sprintf("torrent data %d - %s", i, generateCleanupTestData(budget.historySize))),
		}
		err := database.Gorm().Create(history).Error
		if err != nil {
			t.Fatalf("Failed to create torrent stream history: %v", err)
		}
	}
}

func checkTableCounts(t *testing.T, database *Database, phase string, budget cleanupBudget) {
	var scanCount, localCount, torrentCount int64

	err := database.Gorm().Model(&models.ScanSummary{}).Count(&scanCount).Error
	if err != nil {
		t.Errorf("Failed to count scan summaries: %v", err)
	}

	err = database.Gorm().Model(&models.LocalFiles{}).Count(&localCount).Error
	if err != nil {
		t.Errorf("Failed to count local files: %v", err)
	}

	err = database.Gorm().Model(&models.TorrentstreamHistory{}).Count(&torrentCount).Error
	if err != nil {
		t.Errorf("Failed to count torrent stream history: %v", err)
	}

	t.Logf("Record counts %s: ScanSummary=%d, LocalFiles=%d, TorrentstreamHistory=%d",
		phase, scanCount, localCount, torrentCount)

	if phase != "after cleanup" {
		return
	}

	// The trim policies keep the newest 5 scan summaries / local files and the
	// newest 40 torrent stream history rows; the expectations are derived from
	// the configured row counts so any budget stays honest.
	if budget.scanSummaryRows > 10 {
		//if scanCount > 10 {
		//	t.Errorf("Expected scan summaries to be trimmed to ≤10, got %d", scanCount)
		//}
		//var minScanSummary models.ScanSummary
		//if err := database.Gorm().Order("id asc").First(&minScanSummary).Error; err != nil {
		//	t.Errorf("Failed to get min scan summary: %v", err)
		//} else if minScanSummary.ID != 996 {
		//	t.Errorf("Expected min scan summary ID to be 991, got %d", minScanSummary.ID)
		//}
	}

	if budget.localFileRows > 10 {
		if localCount > 10 {
			t.Errorf("Expected local files to be trimmed to ≤10, got %d", localCount)
		}
		var minLocalFiles models.LocalFiles
		if err := database.Gorm().Order("id asc").First(&minLocalFiles).Error; err != nil {
			t.Errorf("Failed to get min local files: %v", err)
		} else if want := budget.localFileRows - 4; int(minLocalFiles.ID) != want {
			t.Errorf("Expected min local files ID to be %d, got %d", want, minLocalFiles.ID)
		}
	}

	if budget.historyRows > 50 {
		if torrentCount > 50 {
			t.Errorf("Expected torrent stream history to be trimmed to ≤50, got %d", torrentCount)
		}
		var minTorrentHistory models.TorrentstreamHistory
		if err := database.Gorm().Order("id asc").First(&minTorrentHistory).Error; err != nil {
			t.Errorf("Failed to get min torrent history: %v", err)
		} else if want := budget.historyRows - 39; int(minTorrentHistory.ID) != want {
			t.Errorf("Expected min torrent history ID to be %d, got %d", want, minTorrentHistory.ID)
		}
	}
}

func generateCleanupTestData(length int) string {
	data := make([]byte, length)
	for i := range data {
		data[i] = byte('A' + (i % 26))
	}
	return string(data)
}
