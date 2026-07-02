package usage

import "testing"

func TestParseCodeburnStatus(t *testing.T) {
	blob := []byte(`{"currency":"EUR","today":{"cost":30.92,"savings":0,"calls":310},"month":{"cost":1344.41,"savings":0,"calls":9993}}`)
	r, err := ParseCodeburnStatus(blob)
	if err != nil {
		t.Fatal(err)
	}
	if r.Currency != "EUR" || r.Today.Cost != 30.92 || r.Month.Calls != 9993 {
		t.Fatalf("parsed wrong: %+v", r)
	}
}

func TestParseCodeburnStatusRejectsForeignJSON(t *testing.T) {
	if _, err := ParseCodeburnStatus([]byte(`{"hello":"world"}`)); err == nil {
		t.Fatal("expected rejection of non-codeburn JSON")
	}
}
