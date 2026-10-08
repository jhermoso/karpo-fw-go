// Code generated from the C# seed (040-Data/schema/sqlserver/02-semillas.sql, schema rrhh) with
// the same GUIDs. BenefitPercent and RoleTypeId of the C# position types are not ported: the
// first is payroll data, the second was never read.

package domain

import "github.com/jhermoso/karpo-fw-go/pkg/domain/vocab"

// WellKnownPositionStatuses returns the seed of the position status catalog.
func WellKnownPositionStatuses() []PositionStatus {
	return []PositionStatus{
		{ID: MustPositionStatusID("20000000-0000-0000-0001-000000000001"), Name: "Active", Description: "Position is currently active and can be filled", Active: true},
		{ID: MustPositionStatusID("20000000-0000-0000-0001-000000000002"), Name: "Inactive", Description: "Position is no longer in use", Active: true},
		{ID: MustPositionStatusID("20000000-0000-0000-0001-000000000003"), Name: "Vacant", Description: "Position is active but currently not filled", Active: true},
		{ID: MustPositionStatusID("20000000-0000-0000-0001-000000000004"), Name: "Frozen", Description: "Position is temporarily suspended (hiring freeze)", Active: true},
		{ID: MustPositionStatusID("20000000-0000-0000-0001-000000000005"), Name: "Pending Approval", Description: "Position is awaiting approval to be activated", Active: true},
		{ID: MustPositionStatusID("20000000-0000-0000-0001-000000000006"), Name: "Budgeted", Description: "Position is approved in budget but not yet created", Active: true},
	}
}

// WellKnownPositionClasses returns the seed of the position classification catalog.
func WellKnownPositionClasses() []PositionClass {
	return []PositionClass{
		{ID: MustPositionClassID("20000000-0000-0000-0003-000000000001"), Name: "Dirección", Active: true},
		{ID: MustPositionClassID("20000000-0000-0000-0003-000000000002"), Name: "Mando", Active: true},
		{ID: MustPositionClassID("20000000-0000-0000-0003-000000000003"), Name: "Control", Active: true},
		{ID: MustPositionClassID("20000000-0000-0000-0003-000000000004"), Name: "Cumplimiento", Active: true},
		{ID: MustPositionClassID("20000000-0000-0000-0003-000000000005"), Name: "Técnico IT", Active: true},
		{ID: MustPositionClassID("20000000-0000-0000-0003-000000000006"), Name: "Técnico", Active: true},
		{ID: MustPositionClassID("20000000-0000-0000-0003-000000000007"), Name: "Administración", Active: true},
		{ID: MustPositionClassID("20000000-0000-0000-0003-000000000008"), Name: "Caja / Operaciones", Active: true},
		{ID: MustPositionClassID("20000000-0000-0000-0003-000000000009"), Name: "Servicios", Active: true},
	}
}

// WellKnownPositionTypes returns the seed of the position type catalog.
func WellKnownPositionTypes() []PositionType {
	return []PositionType{
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000001"), Title: "Chief Executive Officer", Description: "CEO - Maximum executive authority", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000001"), Title: "Administrativo (general)", Description: "Clase: Administración", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000007"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000002"), Title: "Chief Technology Officer", Description: "CTO - Technology strategy and leadership", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000002"), Title: "Administrativo / Recepción", Description: "Clase: Servicios", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000009"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000003"), Title: "Chief Financial Officer", Description: "CFO - Financial strategy and control", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000003"), Title: "Administrativo Atención al Cliente", Description: "Clase: Administración", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000007"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000004"), Title: "Chief Operating Officer", Description: "COO - Operations management", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000004"), Title: "Administrativo Comercial", Description: "Clase: Administración", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000007"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000005"), Title: "Chief Human Resources Officer", Description: "CHRO - People and culture leadership", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000005"), Title: "Administrativo Legal / Cumplimiento", Description: "Clase: Cumplimiento", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000004"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000006"), Title: "Administrativo PBC/FT", Description: "Clase: Cumplimiento", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000004"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000007"), Title: "Administrativo de Contabilidad", Description: "Clase: Administración", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000007"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000008"), Title: "Administrativo de Pagos y Transferencias", Description: "Clase: Administración", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000007"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000009"), Title: "Administrativo de RRHH", Description: "Clase: Administración", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000007"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000010"), Title: "Director", Description: "Director of a functional area", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000010"), Title: "Administrativo de Tesorería", Description: "Clase: Administración", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000007"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000011"), Title: "Director of Engineering", Description: "Director of software engineering", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000011"), Title: "Auditor Interno", Description: "Clase: Control", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000003"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000012"), Title: "Director of Infrastructure", Description: "Director of IT infrastructure", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000012"), Title: "Auxiliar Administrativo", Description: "Clase: Caja / Operaciones", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000008"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000013"), Title: "Director of Human Resources", Description: "Director of HR operations", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000013"), Title: "Auxiliar Administrativo de Caja", Description: "Clase: Caja / Operaciones", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000008"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000014"), Title: "Director of Finance", Description: "Director of financial operations", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000014"), Title: "Dirección General", Description: "Clase: Dirección", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000001"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000015"), Title: "Dirección de Área", Description: "Clase: Dirección", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000001"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000016"), Title: "Gestor de Proyectos", Description: "Clase: Mando", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000002"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000017"), Title: "Personal de Limpieza", Description: "Clase: Servicios", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000009"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000018"), Title: "Programador Informático", Description: "Clase: Técnico IT", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000005"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000019"), Title: "Responsable General", Description: "Clase: Mando", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000002"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000020"), Title: "Manager", Description: "Generic manager position", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000020"), Title: "Responsable de Departamento", Description: "Clase: Mando", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000002"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000021"), Title: "Engineering Manager", Description: "Manager of engineering team", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000021"), Title: "Responsable de Oficina", Description: "Clase: Mando", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000002"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000022"), Title: "Project Manager", Description: "Manager of projects", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000022"), Title: "Responsable de Red de Oficinas", Description: "Clase: Mando", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000002"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000023"), Title: "Product Manager", Description: "Manager of product development", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0004-000000000023"), Title: "Técnico de Marketing/Web", Description: "Clase: Técnico", Active: true, Classes: []PositionTypeClass{{Class: MustPositionClassID("20000000-0000-0000-0003-000000000006"), StandardWeeklyHours: vocab.Decimal{}}}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000024"), Title: "HR Manager", Description: "Manager of HR team", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000030"), Title: "Tech Lead", Description: "Technical team leader", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000031"), Title: "Tech Lead Backend", Description: "Backend technical leader", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000032"), Title: "Tech Lead Frontend", Description: "Frontend technical leader", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000033"), Title: "Tech Lead DevOps", Description: "DevOps technical leader", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000040"), Title: "Senior Developer", Description: "Senior software developer", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000041"), Title: "Senior Analyst", Description: "Senior business analyst", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000042"), Title: "Senior Designer", Description: "Senior UX/UI designer", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000043"), Title: "Senior SysAdmin", Description: "Senior system administrator", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000050"), Title: "Developer", Description: "Software developer", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000051"), Title: "Analyst", Description: "Business analyst", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000052"), Title: "Designer", Description: "UX/UI designer", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000053"), Title: "SysAdmin", Description: "System administrator", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000060"), Title: "Junior Developer", Description: "Junior software developer", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000061"), Title: "Junior Analyst", Description: "Junior business analyst", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000062"), Title: "Intern", Description: "Internship position", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000070"), Title: "HR Specialist", Description: "Human resources specialist", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000071"), Title: "Financial Controller", Description: "Financial controller", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000072"), Title: "Accountant", Description: "Accountant", Active: false, Classes: []PositionTypeClass{}},
		{ID: MustPositionTypeID("20000000-0000-0000-0002-000000000073"), Title: "Recruiter", Description: "Talent acquisition specialist", Active: false, Classes: []PositionTypeClass{}},
	}
}

// WellKnownAgreements returns the seed of the collective agreement catalog (the C# ConvenioColectivo).
func WellKnownAgreements() []Agreement {
	return []Agreement{
		{ID: MustAgreementID("b3800000-0003-0000-0000-000000000001"), Code: "ES-OFICINAS-DESPACHOS-NAC-2024", Name: "Convenio Colectivo de Oficinas y Despachos (Nacional)", Scope: ScopeNational, TerritorialCode: "", SectoralCode: "82", Start: vocab.MustDate(2024, 1, 1), End: vocab.Date{}, Active: true},
		{ID: MustAgreementID("b3800000-0003-0000-0000-000000000002"), Code: "ES-COMERCIO-GENERAL-2023", Name: "Convenio Colectivo Estatal de Comercio General", Scope: ScopeNational, TerritorialCode: "", SectoralCode: "47", Start: vocab.MustDate(2023, 1, 1), End: vocab.Date{}, Active: true},
		{ID: MustAgreementID("b3800000-0003-0000-0000-000000000003"), Code: "ES-HOSTELERIA-NAC-2024", Name: "Acuerdo Laboral Estatal de Hostelería", Scope: ScopeNational, TerritorialCode: "", SectoralCode: "56", Start: vocab.MustDate(2024, 1, 1), End: vocab.Date{}, Active: true},
		{ID: MustAgreementID("b3800000-0003-0000-0000-000000000004"), Code: "ES-CONSTRUCCION-NAC-2022", Name: "Convenio General de la Construcción", Scope: ScopeNational, TerritorialCode: "", SectoralCode: "41", Start: vocab.MustDate(2022, 1, 1), End: vocab.MustDate(2026, 12, 31), Active: true},
		{ID: MustAgreementID("b3800000-0003-0000-0000-000000000005"), Code: "ES-METAL-NAC-2023", Name: "Convenio Colectivo Estatal del Metal", Scope: ScopeNational, TerritorialCode: "", SectoralCode: "25", Start: vocab.MustDate(2023, 1, 1), End: vocab.Date{}, Active: true},
		{ID: MustAgreementID("b3800000-0003-0000-0000-000000000006"), Code: "ES-QUIMICAS-NAC-2024", Name: "Convenio Colectivo General de la Industria Química", Scope: ScopeNational, TerritorialCode: "", SectoralCode: "20", Start: vocab.MustDate(2024, 1, 1), End: vocab.Date{}, Active: true},
		{ID: MustAgreementID("b3800000-0003-0000-0000-000000000007"), Code: "ES-OFICINAS-DESPACHOS-MADRID-2024", Name: "Oficinas y Despachos de Madrid", Scope: ScopeProvincial, TerritorialCode: "ES30", SectoralCode: "82", Start: vocab.MustDate(2024, 1, 1), End: vocab.Date{}, Active: true},
		{ID: MustAgreementID("b3800000-0003-0000-0000-000000000008"), Code: "ES-TIC-NAC-2024", Name: "Convenio Estatal de Empresas de Consultoría TIC", Scope: ScopeNational, TerritorialCode: "", SectoralCode: "62", Start: vocab.MustDate(2024, 1, 1), End: vocab.Date{}, Active: true},
	}
}
