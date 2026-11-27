// Países de la demostración: código ISO 3166-1 (alfa-2, alfa-3, numérico), nombre en inglés (como los
// sirve Go), prefijo telefónico y una posición aproximada (latitud, longitud) con una clase de tamaño
// que sólo usa el generador de geometría esquemática (scripts/gen-geo.mjs). No es cartografía: son
// datos escritos a mano para la demostración, sin ficheros de terceros.
//
// Clase de tamaño: S pequeño, M mediano, L grande, X enorme.
// Son 248, como los perfiles de país de Go (se omite la Antártida, sin prefijo ni código postal).

const TABLE = `
AD|AND|020|Andorra|376|42.5|1.5|S
AE|ARE|784|United Arab Emirates|971|24|54|M
AF|AFG|004|Afghanistan|93|33.9|67.7|L
AG|ATG|028|Antigua and Barbuda|1268|17.1|-61.8|S
AI|AIA|660|Anguilla|1264|18.2|-63.1|S
AL|ALB|008|Albania|355|41.1|20.2|S
AM|ARM|051|Armenia|374|40.1|45|S
AO|AGO|024|Angola|244|-11.2|17.9|L
AR|ARG|032|Argentina|54|-38.4|-63.6|X
AS|ASM|016|American Samoa|1684|-14.3|-170.7|S
AT|AUT|040|Austria|43|47.5|14.5|S
AU|AUS|036|Australia|61|-25.3|133.8|X
AW|ABW|533|Aruba|297|12.5|-70|S
AX|ALA|248|Åland Islands|358|60.2|20|S
AZ|AZE|031|Azerbaijan|994|40.1|47.6|S
BA|BIH|070|Bosnia and Herzegovina|387|43.9|17.7|S
BB|BRB|052|Barbados|1246|13.2|-59.5|S
BD|BGD|050|Bangladesh|880|23.7|90.4|M
BE|BEL|056|Belgium|32|50.5|4.5|S
BF|BFA|854|Burkina Faso|226|12.2|-1.6|M
BG|BGR|100|Bulgaria|359|42.7|25.5|S
BH|BHR|048|Bahrain|973|26|50.6|S
BI|BDI|108|Burundi|257|-3.4|29.9|S
BJ|BEN|204|Benin|229|9.3|2.3|M
BL|BLM|652|Saint Barthélemy|590|17.9|-62.8|S
BM|BMU|060|Bermuda|1441|32.3|-64.8|S
BN|BRN|096|Brunei Darussalam|673|4.5|114.7|S
BO|BOL|068|Bolivia|591|-16.3|-63.6|L
BQ|BES|535|Bonaire, Sint Eustatius and Saba|599|12.2|-68.3|S
BR|BRA|076|Brazil|55|-14.2|-51.9|X
BS|BHS|044|Bahamas|1242|25|-77.4|S
BT|BTN|064|Bhutan|975|27.5|90.4|S
BV|BVT|074|Bouvet Island|47|-54.4|3.4|S
BW|BWA|072|Botswana|267|-22.3|24.7|M
BY|BLR|112|Belarus|375|53.7|27.9|M
BZ|BLZ|084|Belize|501|17.2|-88.5|S
CA|CAN|124|Canada|1|56.1|-106.3|X
CC|CCK|166|Cocos (Keeling) Islands|61|-12.2|96.8|S
CD|COD|180|Congo, Democratic Republic of the|243|-4|21.8|X
CF|CAF|140|Central African Republic|236|6.6|20.9|L
CG|COG|178|Congo|242|-0.2|15.8|M
CH|CHE|756|Switzerland|41|46.8|8.2|S
CI|CIV|384|Côte d'Ivoire|225|7.5|-5.5|M
CK|COK|184|Cook Islands|682|-21.2|-159.8|S
CL|CHL|152|Chile|56|-35.7|-71.5|L
CM|CMR|120|Cameroon|237|7.4|12.4|M
CN|CHN|156|China|86|35.9|104.2|X
CO|COL|170|Colombia|57|4.6|-74.3|L
CR|CRI|188|Costa Rica|506|9.7|-83.8|S
CU|CUB|192|Cuba|53|21.5|-77.8|M
CV|CPV|132|Cabo Verde|238|16|-24|S
CW|CUW|531|Curaçao|599|12.2|-69|S
CX|CXR|162|Christmas Island|61|-10.5|105.7|S
CY|CYP|196|Cyprus|357|35.1|33.4|S
CZ|CZE|203|Czechia|420|49.8|15.5|S
DE|DEU|276|Germany|49|51.2|10.5|M
DJ|DJI|262|Djibouti|253|11.8|42.6|S
DK|DNK|208|Denmark|45|56.3|9.5|S
DM|DMA|212|Dominica|1767|15.4|-61.4|S
DO|DOM|214|Dominican Republic|1809|18.7|-70.2|S
DZ|DZA|012|Algeria|213|28|1.7|X
EC|ECU|218|Ecuador|593|-1.8|-78.2|M
EE|EST|233|Estonia|372|58.6|25|S
EG|EGY|818|Egypt|20|26.8|30.8|L
EH|ESH|732|Western Sahara|212|24.2|-12.9|M
ER|ERI|232|Eritrea|291|15.2|39.8|M
ES|ESP|724|Spain|34|40.4|-3.7|L
ET|ETH|231|Ethiopia|251|9.1|40.5|L
FI|FIN|246|Finland|358|61.9|25.7|M
FJ|FJI|242|Fiji|679|-17.7|178.1|S
FK|FLK|238|Falkland Islands (Malvinas)|500|-51.8|-59.5|S
FM|FSM|583|Micronesia, Federated States of|691|7.4|150.6|S
FO|FRO|234|Faroe Islands|298|61.9|-6.9|S
FR|FRA|250|France|33|46.2|2.2|M
GA|GAB|266|Gabon|241|-0.8|11.6|M
GB|GBR|826|United Kingdom|44|55.4|-3.4|M
GD|GRD|308|Grenada|1473|12.1|-61.7|S
GE|GEO|268|Georgia|995|42.3|43.4|S
GF|GUF|254|French Guiana|594|3.9|-53.1|S
GG|GGY|831|Guernsey|44|49.5|-2.6|S
GH|GHA|288|Ghana|233|7.9|-1|M
GI|GIB|292|Gibraltar|350|36.1|-5.3|S
GL|GRL|304|Greenland|299|71.7|-42.6|X
GM|GMB|270|Gambia|220|13.4|-15.3|S
GN|GIN|324|Guinea|224|9.9|-9.7|M
GP|GLP|312|Guadeloupe|590|16.3|-61.6|S
GQ|GNQ|226|Equatorial Guinea|240|1.7|10.3|S
GR|GRC|300|Greece|30|39.1|21.8|S
GS|SGS|239|South Georgia and the South Sandwich Islands|500|-54.4|-36.6|S
GT|GTM|320|Guatemala|502|15.8|-90.2|S
GU|GUM|316|Guam|1671|13.4|144.8|S
GW|GNB|624|Guinea-Bissau|245|11.8|-15.2|S
GY|GUY|328|Guyana|592|4.9|-58.9|M
HK|HKG|344|Hong Kong|852|22.4|114.1|S
HM|HMD|334|Heard Island and McDonald Islands|672|-53.1|73.5|S
HN|HND|340|Honduras|504|15.2|-86.2|M
HR|HRV|191|Croatia|385|45.1|15.2|S
HT|HTI|332|Haiti|509|18.9|-72.3|S
HU|HUN|348|Hungary|36|47.2|19.5|S
ID|IDN|360|Indonesia|62|-0.8|113.9|X
IE|IRL|372|Ireland|353|53.4|-8.2|S
IL|ISR|376|Israel|972|31|34.9|S
IM|IMN|833|Isle of Man|44|54.2|-4.5|S
IN|IND|356|India|91|20.6|79|X
IO|IOT|086|British Indian Ocean Territory|246|-6.3|71.9|S
IQ|IRQ|368|Iraq|964|33.2|43.7|M
IR|IRN|364|Iran|98|32.4|53.7|L
IS|ISL|352|Iceland|354|64.9|-19|M
IT|ITA|380|Italy|39|41.9|12.6|M
JE|JEY|832|Jersey|44|49.2|-2.1|S
JM|JAM|388|Jamaica|1876|18.1|-77.3|S
JO|JOR|400|Jordan|962|30.6|36.2|S
JP|JPN|392|Japan|81|36.2|138.3|L
KE|KEN|404|Kenya|254|0|37.9|M
KG|KGZ|417|Kyrgyzstan|996|41.2|74.8|M
KH|KHM|116|Cambodia|855|12.6|104.9|S
KI|KIR|296|Kiribati|686|-3.4|-168.7|S
KM|COM|174|Comoros|269|-11.9|43.9|S
KN|KNA|659|Saint Kitts and Nevis|1869|17.4|-62.8|S
KP|PRK|408|Korea, Democratic People's Republic of|850|40.3|127.5|M
KR|KOR|410|Korea, Republic of|82|35.9|127.8|M
KW|KWT|414|Kuwait|965|29.3|47.5|S
KY|CYM|136|Cayman Islands|1345|19.3|-81.3|S
KZ|KAZ|398|Kazakhstan|7|48|66.9|X
LA|LAO|418|Lao People's Democratic Republic|856|19.9|102.5|M
LB|LBN|422|Lebanon|961|33.9|35.9|S
LC|LCA|662|Saint Lucia|1758|13.9|-61|S
LI|LIE|438|Liechtenstein|423|47.2|9.6|S
LK|LKA|144|Sri Lanka|94|7.9|80.8|S
LR|LBR|430|Liberia|231|6.4|-9.4|S
LS|LSO|426|Lesotho|266|-29.6|28.2|S
LT|LTU|440|Lithuania|370|55.2|23.9|S
LU|LUX|442|Luxembourg|352|49.8|6.1|S
LV|LVA|428|Latvia|371|56.9|24.6|S
LY|LBY|434|Libya|218|26.3|17.2|X
MA|MAR|504|Morocco|212|31.8|-7.1|M
MC|MCO|492|Monaco|377|43.7|7.4|S
MD|MDA|498|Moldova|373|47.4|28.4|S
ME|MNE|499|Montenegro|382|42.7|19.4|S
MF|MAF|663|Saint Martin (French part)|590|18.1|-63.1|S
MG|MDG|450|Madagascar|261|-18.8|46.9|L
MH|MHL|584|Marshall Islands|692|7.1|171.2|S
MK|MKD|807|North Macedonia|389|41.6|21.7|S
ML|MLI|466|Mali|223|17.6|-4|L
MM|MMR|104|Myanmar|95|21.9|96|M
MN|MNG|496|Mongolia|976|46.9|103.8|L
MO|MAC|446|Macao|853|22.2|113.5|S
MP|MNP|580|Northern Mariana Islands|1670|15.1|145.7|S
MQ|MTQ|474|Martinique|596|14.6|-61|S
MR|MRT|478|Mauritania|222|21|-10.9|L
MS|MSR|500|Montserrat|1664|16.7|-62.2|S
MT|MLT|470|Malta|356|35.9|14.4|S
MU|MUS|480|Mauritius|230|-20.3|57.6|S
MV|MDV|462|Maldives|960|3.2|73.2|S
MW|MWI|454|Malawi|265|-13.3|34.3|S
MX|MEX|484|Mexico|52|23.6|-102.6|X
MY|MYS|458|Malaysia|60|4.2|102|M
MZ|MOZ|508|Mozambique|258|-18.7|35.5|L
NA|NAM|516|Namibia|264|-23|18.5|L
NC|NCL|540|New Caledonia|687|-20.9|165.6|S
NE|NER|562|Niger|227|17.6|8.1|L
NF|NFK|574|Norfolk Island|672|-29|168|S
NG|NGA|566|Nigeria|234|9.1|8.7|L
NI|NIC|558|Nicaragua|505|12.9|-85.2|M
NL|NLD|528|Netherlands|31|52.1|5.3|S
NO|NOR|578|Norway|47|60.5|8.5|M
NP|NPL|524|Nepal|977|28.4|84.1|M
NR|NRU|520|Nauru|674|-0.5|166.9|S
NU|NIU|570|Niue|683|-19|-169.9|S
NZ|NZL|554|New Zealand|64|-40.9|174.9|M
OM|OMN|512|Oman|968|21.5|55.9|M
PA|PAN|591|Panama|507|8.5|-80.8|S
PE|PER|604|Peru|51|-9.2|-75|L
PF|PYF|258|French Polynesia|689|-17.7|-149.4|S
PG|PNG|598|Papua New Guinea|675|-6.3|144|M
PH|PHL|608|Philippines|63|12.9|121.8|M
PK|PAK|586|Pakistan|92|30.4|69.3|L
PL|POL|616|Poland|48|51.9|19.1|M
PM|SPM|666|Saint Pierre and Miquelon|508|46.9|-56.3|S
PN|PCN|612|Pitcairn|64|-24.7|-127.4|S
PR|PRI|630|Puerto Rico|1787|18.2|-66.6|S
PS|PSE|275|Palestine, State of|970|32|35.2|S
PT|PRT|620|Portugal|351|39.4|-8.2|S
PW|PLW|585|Palau|680|7.5|134.6|S
PY|PRY|600|Paraguay|595|-23.4|-58.4|M
QA|QAT|634|Qatar|974|25.4|51.2|S
RE|REU|638|Réunion|262|-21.1|55.5|S
RO|ROU|642|Romania|40|45.9|25|M
RS|SRB|688|Serbia|381|44|21|S
RU|RUS|643|Russian Federation|7|61.5|105.3|X
RW|RWA|646|Rwanda|250|-1.9|29.9|S
SA|SAU|682|Saudi Arabia|966|23.9|45.1|L
SB|SLB|090|Solomon Islands|677|-9.6|160.2|S
SC|SYC|690|Seychelles|248|-4.7|55.5|S
SD|SDN|729|Sudan|249|12.9|30.2|L
SE|SWE|752|Sweden|46|60.1|18.6|M
SG|SGP|702|Singapore|65|1.4|103.8|S
SH|SHN|654|Saint Helena, Ascension and Tristan da Cunha|290|-24.1|-10|S
SI|SVN|705|Slovenia|386|46.2|15|S
SJ|SJM|744|Svalbard and Jan Mayen|47|77.6|23.7|S
SK|SVK|703|Slovakia|421|48.7|19.7|S
SL|SLE|694|Sierra Leone|232|8.5|-11.8|S
SM|SMR|674|San Marino|378|43.9|12.5|S
SN|SEN|686|Senegal|221|14.5|-14.5|M
SO|SOM|706|Somalia|252|5.2|46.2|L
SR|SUR|740|Suriname|597|3.9|-56|M
SS|SSD|728|South Sudan|211|6.9|31.3|M
ST|STP|678|Sao Tome and Principe|239|0.2|6.6|S
SV|SLV|222|El Salvador|503|13.8|-88.9|S
SX|SXM|534|Sint Maarten (Dutch part)|1721|18|-63.1|S
SY|SYR|760|Syrian Arab Republic|963|34.8|39|M
SZ|SWZ|748|Eswatini|268|-26.5|31.5|S
TC|TCA|796|Turks and Caicos Islands|1649|21.7|-71.8|S
TD|TCD|148|Chad|235|15.5|18.7|L
TF|ATF|260|French Southern Territories|262|-49.3|69.3|S
TG|TGO|768|Togo|228|8.6|0.8|S
TH|THA|764|Thailand|66|15.9|101|M
TJ|TJK|762|Tajikistan|992|38.9|71.3|S
TK|TKL|772|Tokelau|690|-9.2|-171.8|S
TL|TLS|626|Timor-Leste|670|-8.9|125.7|S
TM|TKM|795|Turkmenistan|993|39|59.6|M
TN|TUN|788|Tunisia|216|33.9|9.5|S
TO|TON|776|Tonga|676|-21.2|-175.2|S
TR|TUR|792|Türkiye|90|39|35.2|L
TT|TTO|780|Trinidad and Tobago|1868|10.7|-61.2|S
TV|TUV|798|Tuvalu|688|-7.1|177.6|S
TW|TWN|158|Taiwan|886|23.7|121|S
TZ|TZA|834|Tanzania|255|-6.4|34.9|L
UA|UKR|804|Ukraine|380|48.4|31.2|M
UG|UGA|800|Uganda|256|1.4|32.3|M
UM|UMI|581|United States Minor Outlying Islands|1|19.3|166.6|S
US|USA|840|United States of America|1|37.1|-95.7|X
UY|URY|858|Uruguay|598|-32.5|-55.8|S
UZ|UZB|860|Uzbekistan|998|41.4|64.6|L
VA|VAT|336|Holy See|379|41.9|12.5|S
VC|VCT|670|Saint Vincent and the Grenadines|1784|13|-61.3|S
VE|VEN|862|Venezuela|58|6.4|-66.6|L
VG|VGB|092|Virgin Islands (British)|1284|18.4|-64.6|S
VI|VIR|850|Virgin Islands (U.S.)|1340|18.3|-64.9|S
VN|VNM|704|Viet Nam|84|14.1|108.3|M
VU|VUT|548|Vanuatu|678|-15.4|167|S
WF|WLF|876|Wallis and Futuna|681|-13.8|-177.2|S
WS|WSM|882|Samoa|685|-13.8|-172.1|S
YE|YEM|887|Yemen|967|15.6|48.5|M
YT|MYT|175|Mayotte|262|-12.8|45.2|S
ZA|ZAF|710|South Africa|27|-30.6|22.9|L
ZM|ZMB|894|Zambia|260|-13.1|27.8|L
ZW|ZWE|716|Zimbabwe|263|-19|29.2|M
`;

const EU = new Set('AT BE BG HR CY CZ DK EE FI FR DE GR HU IE IT LV LT LU MT NL PL PT RO SK SI ES SE'.split(' '));
const EUROZONE = new Set('AT BE BG HR CY EE FI FR DE GR IE IT LV LT LU MT NL PT SK SI ES'.split(' '));
const SEPA_EXTRA = new Set('IS LI NO CH GB MC SM AD VA'.split(' '));
const EEA_EXTRA = new Set('IS LI NO'.split(' '));

/**
 * @typedef {{ alpha2: string, alpha3: string, numeric: number, name: string, callingCode: string,
 *   lat: number, lon: number, size: 'S' | 'M' | 'L' | 'X', eu: boolean, eurozone: boolean,
 *   sepa: boolean, eea: boolean }} CountrySeed
 */

/** @type {CountrySeed[]} */
export const COUNTRY_SEEDS = TABLE.trim()
  .split('\n')
  .map((line) => {
    const [alpha2, alpha3, numeric, name, calling, lat, lon, size] = line.split('|');
    return {
      alpha2,
      alpha3,
      numeric: Number(numeric),
      name,
      callingCode: `+${calling}`,
      lat: Number(lat),
      lon: Number(lon),
      size,
      eu: EU.has(alpha2),
      eurozone: EUROZONE.has(alpha2),
      sepa: EU.has(alpha2) || SEPA_EXTRA.has(alpha2),
      eea: EU.has(alpha2) || EEA_EXTRA.has(alpha2),
    };
  });
