package quality

// Types matching xykt/IPQuality's JSON output schema exactly.
// All fields use pointers to distinguish "null" from "zero".

// Head is the report header.
type Head struct {
	IP      string `json:"IP"`
	Command string `json:"Command,omitempty"`
	GitHub  string `json:"GitHub,omitempty"`
	Time    string `json:"Time"`
	Version string `json:"Version"`
}

// Info is the basic geographic/ASN information.
type Info struct {
	ASN              *string       `json:"ASN"`
	Organization     *string       `json:"Organization"`
	Latitude         *string       `json:"Latitude"`
	Longitude        *string       `json:"Longitude"`
	DMS              *string       `json:"DMS"`
	Map              *string       `json:"Map"`
	TimeZone         *string       `json:"TimeZone"`
	Type             *string       `json:"Type"` // geo-consistent / discrepant / null
	City             CityInfo      `json:"City"`
	Region           RegionInfo    `json:"Region"`
	Continent        ContinentInfo `json:"Continent"`
	RegisteredRegion RegionInfo    `json:"RegisteredRegion"`
}

type CityInfo struct {
	Name         *string `json:"Name"`
	PostalCode   *string `json:"PostalCode"`
	SubCode      *string `json:"SubCode"`
	Subdivisions *string `json:"Subdivisions"`
}

type RegionInfo struct {
	Code *string `json:"Code"`
	Name *string `json:"Name"`
}

type ContinentInfo struct {
	Code *string `json:"Code"`
	Name *string `json:"Name"`
}

// Type is the IP usage/company classification.
type Type struct {
	Usage   map[string]*string `json:"Usage"`
	Company map[string]*string `json:"Company"`
}

// Score is the risk scores (0 = best).
type Score struct {
	IP2LOCATION *string `json:"IP2LOCATION"`
	SCAMALYTICS *string `json:"SCAMALYTICS"`
	Ipapi       *string `json:"ipapi"`
	AbuseIPDB   *string `json:"AbuseIPDB"`
	IPQS        *string `json:"IPQS"`
	DBIP        *string `json:"DBIP"`
}

// Factor is the risk factor boolean matrix. For each dimension, a map
// of source → bool/null.
type Factor struct {
	CountryCode map[string]*string `json:"CountryCode"`
	Proxy       map[string]*string `json:"Proxy"`
	Tor         map[string]*string `json:"Tor"`
	VPN         map[string]*string `json:"VPN"`
	Server      map[string]*string `json:"Server"`
	Abuser      map[string]*string `json:"Abuser"`
	Robot       map[string]*string `json:"Robot"`
}

// Media is the streaming/AI unlock test results.
type Media struct {
	TikTok           MediaEntry `json:"TikTok"`
	DisneyPlus       MediaEntry `json:"DisneyPlus"`
	Netflix          MediaEntry `json:"Netflix"`
	Youtube          MediaEntry `json:"Youtube"`
	AmazonPrimeVideo MediaEntry `json:"AmazonPrimeVideo"`
	Reddit           MediaEntry `json:"Reddit"`
	ChatGPT          MediaEntry `json:"ChatGPT"`
}

type MediaEntry struct {
	Status *string `json:"Status"`
	Region *string `json:"Region"`
	Type   *string `json:"Type"`
}

// Mail is the mail connectivity and DNSBL results.
type Mail struct {
	Port25       *bool `json:"Port25"`
	Gmail        *bool `json:"Gmail"`
	Outlook      *bool `json:"Outlook"`
	Yahoo        *bool `json:"Yahoo"`
	Apple        *bool `json:"Apple"`
	QQ           *bool `json:"QQ"`
	MailRU       *bool `json:"MailRU"`
	AOL          *bool `json:"AOL"`
	GMX          *bool `json:"GMX"`
	MailCOM      *bool `json:"MailCOM"`
	M163         *bool `json:"163"`
	Sohu         *bool `json:"Sohu"`
	Sina         *bool `json:"Sina"`
	DNSBlacklist DNSBL `json:"DNSBlacklist"`
}

type DNSBL struct {
	Total       *int `json:"Total"`
	Clean       *int `json:"Clean"`
	Marked      *int `json:"Marked"`
	Blacklisted *int `json:"Blacklisted"`
}
