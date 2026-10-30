package domain

import (
	"maps"
	"slices"
	"strings"

	fw "github.com/jhermoso/karpo-fw-go/pkg/domain"
	"github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"
)

// Identidad y perfil (docs/DECISIONES-COMUNES.md, D-121 y D-122): la identidad de una party es lo
// que es verdad para todas las empresas y lo acredita un documento o un registro público (nombre o
// razón social, documentos, forma jurídica, nacimiento, sexo, estado civil, idioma, contactos de
// todos); lo de cada empresa va en el perfil de su relación con la party.

// Language devuelve el idioma preferido de la party (cero si no se sabe).
func (p *Party) Language() vocab.LanguageCode { return p.language }

// SetPreferredLanguage fija el idioma preferido de la party, o lo quita con el código cero.
func (p *Party) SetPreferredLanguage(l vocab.LanguageCode) error {
	if err := p.requireActive("change its language"); err != nil {
		return err
	}
	if l == p.language {
		return nil
	}
	p.language = l
	p.Raise(PreferredLanguageChanged{EventMeta: p.NewEventMeta(), Language: l.String()})
	return nil
}

// MarkPrimaryIdentification hace principal un documento de la party; el que lo era deja de serlo.
func (p *Party) MarkPrimaryIdentification(id IdentificationID) error {
	k := slices.IndexFunc(p.identifications, func(i Identification) bool { return i.ID == id })
	if k < 0 {
		return fw.NotFound("parties.identification", id)
	}
	if err := p.requireActive("change its identifications"); err != nil {
		return err
	}
	if p.identifications[k].Primary {
		return nil
	}
	ids := slices.Clone(p.identifications)
	for i := range ids {
		ids[i].Primary = i == k
	}
	p.identifications = ids
	p.Raise(PrimaryIdentificationChanged{EventMeta: p.NewEventMeta(), IdentificationID: id.String()})
	return nil
}

// Campos de la identidad que sólo corrigen la propia empresa interna o un administrador
// (D-122, regla 3): el documento de identidad y el nombre o la razón social.
const (
	IdentityFieldIdentifications = "identifications"
	IdentityFieldName            = "name"
)

// IdentityFields devuelve la identidad de la party, campo a campo, en texto: lo que se compara
// antes y después de un cambio para saber qué se corrigió (NoteIdentityCorrections). Los contactos
// son los de todos que no han terminado; los de una empresa son de su perfil y no cuentan.
func (p *Party) IdentityFields() map[string]string {
	f := map[string]string{"language": p.language.String()}
	if p.kind == KindPerson {
		n := p.person.Name
		f[IdentityFieldName] = n.Given() + "|" + n.FirstSurname() + "|" + n.SecondSurname()
		f["gender"], f["birthDate"], f["maritalStatus"] = string(p.person.Gender), p.person.BirthDate.String(), string(p.person.MaritalStatus)
	} else {
		f[IdentityFieldName] = p.organization.Name.Legal()
		f["tradeName"], f["legalForm"] = p.organization.Name.Trade(), string(p.organization.LegalForm)
	}
	docs := make([]string, 0, len(p.identifications))
	for _, i := range p.identifications {
		d := i.Type.String() + ":" + i.Country.String() + ":" + i.Number
		if i.Primary {
			d += "*"
		}
		docs = append(docs, d)
	}
	slices.Sort(docs)
	f[IdentityFieldIdentifications] = strings.Join(docs, ",")
	if i := p.documentIssue; !i.IsZero() { // la marca de documento rechazado también es del documento
		f["documentIssue"] = i.Number + "|" + i.Reason
	} else {
		f["documentIssue"] = ""
	}
	now := fw.Now()
	var contacts []string
	for _, c := range p.contacts {
		if end, closed := c.Period.To(); c.IsOwned() || (closed && !end.After(now)) {
			continue
		}
		contacts = append(contacts, c.ID.String()+":"+string(c.Kind)+":"+c.Value+c.Address.String())
	}
	slices.Sort(contacts)
	f["contacts"] = strings.Join(contacts, ",")
	return f
}

// ownerSuffix es lo que el registro de auditoría añade a un contacto con dueña: «@» y la dueña.
func ownerSuffix(c Contact) string {
	if !c.IsOwned() {
		return ""
	}
	return "@" + c.Owner.String()
}

// NoteIdentityCorrections compara la identidad con la de antes de un cambio y, si algo cambió, lanza
// IdentityCorrected con los campos que cambiaron, para avisar a las empresas que se relacionan con
// la party (D-122, regla 3). Devuelve los campos.
func (p *Party) NoteIdentityCorrections(before map[string]string) []string {
	after := p.IdentityFields()
	var changed []string
	for _, k := range slices.Sorted(maps.Keys(after)) {
		// Dar el primer documento a una party que no tenía ninguno completa su identidad, no la corrige
		// (docs/PARTIES.md §23): no avisa.
		if k == IdentityFieldIdentifications && before[k] == "" {
			continue
		}
		if before[k] != after[k] {
			changed = append(changed, k)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	orgs := []string{}
	for _, o := range p.OrganizationsAt(fw.Now()) {
		orgs = append(orgs, o.String())
	}
	p.Raise(IdentityCorrected{EventMeta: p.NewEventMeta(), Fields: changed, Organizations: orgs})
	return changed
}
