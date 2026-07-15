package quality

import (
	"fmt"
	"strings"
)

// assembleReport creates a Telegram Markdown report from the result.
func assembleReport(r *Result) string {
	var sb strings.Builder

	// Header.
	sb.WriteString(fmt.Sprintf("*IP Quality Report*\nIP: `%s`\nTime: %s\n\n",
		r.Head.IP, r.Head.Time))

	// Info section.
	sb.WriteString("*Info*\n")
	if r.Info.ASN != nil {
		sb.WriteString(fmt.Sprintf("ASN: %s\n", *r.Info.ASN))
	}
	if r.Info.Organization != nil {
		sb.WriteString(fmt.Sprintf("Org: %s\n", *r.Info.Organization))
	}
	if r.Info.Region.Code != nil {
		sb.WriteString(fmt.Sprintf("Region: %s (%s)\n", ptrVal(r.Info.Region.Name), *r.Info.Region.Code))
	}
	if r.Info.City.Name != nil {
		sb.WriteString(fmt.Sprintf("City: %s\n", *r.Info.City.Name))
	}
	if r.Info.Type != nil {
		sb.WriteString(fmt.Sprintf("Geo: %s\n", *r.Info.Type))
	}
	sb.WriteString("\n")

	// Score section.
	sb.WriteString("*Risk Scores*\n")
	sb.WriteString(formatScore("IP2Location", r.Score.IP2LOCATION))
	sb.WriteString(formatScore("Scamalytics", r.Score.SCAMALYTICS))
	sb.WriteString(formatScore("ipapi.is", r.Score.Ipapi))
	sb.WriteString(formatScore("AbuseIPDB", r.Score.AbuseIPDB))
	sb.WriteString(formatScore("IPQS", r.Score.IPQS))
	sb.WriteString(formatScore("DB-IP", r.Score.DBIP))
	sb.WriteString("\n")

	// Factor section.
	sb.WriteString("*Risk Factors*\n")
	sb.WriteString(formatFactor("Proxy", r.Factor.Proxy))
	sb.WriteString(formatFactor("VPN", r.Factor.VPN))
	sb.WriteString(formatFactor("Tor", r.Factor.Tor))
	sb.WriteString(formatFactor("Server", r.Factor.Server))
	sb.WriteString(formatFactor("Abuser", r.Factor.Abuser))
	sb.WriteString("\n")

	// Media section.
	sb.WriteString("*Media Unlock*\n")
	sb.WriteString(formatMedia("TikTok", r.Media.TikTok))
	sb.WriteString(formatMedia("Disney+", r.Media.DisneyPlus))
	sb.WriteString(formatMedia("Netflix", r.Media.Netflix))
	sb.WriteString(formatMedia("YouTube", r.Media.Youtube))
	sb.WriteString(formatMedia("Prime Video", r.Media.AmazonPrimeVideo))
	sb.WriteString(formatMedia("Reddit", r.Media.Reddit))
	sb.WriteString(formatMedia("ChatGPT", r.Media.ChatGPT))
	sb.WriteString("\n")

	// Mail section.
	sb.WriteString("*Mail*\n")
	if r.Mail.Port25 != nil {
		sb.WriteString(fmt.Sprintf("Port25: %v\n", *r.Mail.Port25))
	}
	for _, name := range mailProviderOrder {
		val := getMailField(r.Mail, name)
		if val != nil {
			sb.WriteString(fmt.Sprintf("%s: %v\n", name, *val))
		}
	}
	if r.Mail.DNSBlacklist.Total != nil {
		sb.WriteString(fmt.Sprintf("DNSBL: %d total, %d clean, %d marked, %d blacklisted\n",
			*r.Mail.DNSBlacklist.Total, ptrIntVal(r.Mail.DNSBlacklist.Clean),
			ptrIntVal(r.Mail.DNSBlacklist.Marked), ptrIntVal(r.Mail.DNSBlacklist.Blacklisted)))
	}

	return sb.String()
}

func formatScore(name string, val *string) string {
	if val == nil {
		return fmt.Sprintf("%s: N/A\n", name)
	}
	return fmt.Sprintf("%s: %s\n", name, *val)
}

func formatFactor(name string, m map[string]*string) string {
	yes := 0
	no := 0
	unknown := 0
	for _, v := range m {
		if v == nil {
			unknown++
		} else if *v == "true" {
			yes++
		} else if *v == "false" {
			no++
		} else {
			unknown++
		}
	}
	return fmt.Sprintf("%s: %d yes / %d no / %d unknown\n", name, yes, no, unknown)
}

func formatMedia(name string, e MediaEntry) string {
	status := "N/A"
	region := ""
	if e.Status != nil {
		status = *e.Status
	}
	if e.Region != nil {
		region = *e.Region
	}
	return fmt.Sprintf("%s: %s %s\n", name, status, region)
}

func getMailField(m Mail, name string) *bool {
	switch name {
	case "Gmail":
		return m.Gmail
	case "Outlook":
		return m.Outlook
	case "Yahoo":
		return m.Yahoo
	case "Apple":
		return m.Apple
	case "QQ":
		return m.QQ
	case "MailRU":
		return m.MailRU
	case "AOL":
		return m.AOL
	case "GMX":
		return m.GMX
	case "MailCOM":
		return m.MailCOM
	case "163":
		return m.M163
	case "Sohu":
		return m.Sohu
	case "Sina":
		return m.Sina
	}
	return nil
}

func ptrVal(s *string) string {
	if s == nil {
		return "N/A"
	}
	return *s
}

func ptrIntVal(i *int) int {
	if i == nil {
		return 0
	}
	return *i
}
