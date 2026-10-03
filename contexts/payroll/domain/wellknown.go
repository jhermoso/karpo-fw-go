package domain

// WellKnownConcepts returns the seed of the pay concept catalog. The deductions keep the GUIDs of
// the C# rrhh.deduction_type rows (B3800000-0001-…); the rest are new (B3800000-0004-…). The
// catalog carries no rates: there is no calculation engine (see docs/NOMINAS.md).
func WellKnownConcepts() []Concept {
	earning := func(id, code, name string, contributable, taxable bool) Concept {
		return Concept{ID: MustConceptID(id), Code: code, Name: name, Kind: KindEarning, Contributable: contributable, Taxable: taxable,
			PerceptionKey: "A", Active: true}
	}
	other := func(id, code, name string, kind ConceptKind) Concept {
		return Concept{ID: MustConceptID(id), Code: code, Name: name, Kind: kind, Active: true}
	}
	return []Concept{
		// Deductions of the C# seed (same GUIDs).
		other("b3800000-0001-0000-0000-000000000001", "IRPF_GEN", "Retención IRPF general", KindIncomeTax),
		other("b3800000-0001-0000-0000-000000000002", "IRPF_PROF", "Retención IRPF actividades profesionales", KindIncomeTax),
		other("b3800000-0001-0000-0000-000000000003", "IRPF_AGR", "Retención IRPF actividades agrícolas", KindIncomeTax),
		other("b3800000-0001-0000-0000-000000000004", "IRPF_NR", "Retención IRNR no residentes", KindIncomeTax),
		other("b3800000-0001-0000-0000-000000000005", "SS_EMP_CG", "SS trabajador contingencias comunes", KindSocialSecurity),
		other("b3800000-0001-0000-0000-000000000006", "SS_EMP_DES", "SS trabajador desempleo", KindSocialSecurity),
		other("b3800000-0001-0000-0000-000000000007", "SS_EMP_FP", "SS trabajador formación profesional", KindSocialSecurity),
		other("b3800000-0001-0000-0000-000000000008", "EMBARGO", "Embargo judicial", KindDeduction),
		// Earnings.
		earning("b3800000-0004-0000-0000-000000000001", "SALARIO_BASE", "Salario base", true, true),
		earning("b3800000-0004-0000-0000-000000000002", "PLUS_CONVENIO", "Plus de convenio", true, true),
		earning("b3800000-0004-0000-0000-000000000003", "COMPLEMENTO", "Complemento personal", true, true),
		earning("b3800000-0004-0000-0000-000000000004", "HORAS_EXTRA", "Horas extraordinarias", true, true),
		earning("b3800000-0004-0000-0000-000000000005", "PAGA_EXTRA", "Paga extraordinaria", true, true),
		earning("b3800000-0004-0000-0000-000000000006", "PRORRATA_EXTRA", "Prorrata de pagas extraordinarias", true, true),
		earning("b3800000-0004-0000-0000-000000000007", "VACACIONES", "Vacaciones no disfrutadas", true, true),
		// Other employee contributions and deductions.
		other("b3800000-0004-0000-0000-000000000101", "SS_EMP_MEI", "SS trabajador mecanismo de equidad intergeneracional", KindSocialSecurity),
		other("b3800000-0004-0000-0000-000000000102", "SS_EMP_HEX", "SS trabajador horas extraordinarias", KindSocialSecurity),
		other("b3800000-0004-0000-0000-000000000103", "ANTICIPO", "Anticipo", KindDeduction),
		// Employer contributions (company cost).
		other("b3800000-0004-0000-0000-000000000201", "SS_ER_CC", "SS empresa contingencias comunes", KindEmployerCost),
		other("b3800000-0004-0000-0000-000000000202", "SS_ER_ATEP", "SS empresa accidentes de trabajo y enfermedad profesional", KindEmployerCost),
		other("b3800000-0004-0000-0000-000000000203", "SS_ER_DES", "SS empresa desempleo", KindEmployerCost),
		other("b3800000-0004-0000-0000-000000000204", "SS_ER_FOGASA", "SS empresa FOGASA", KindEmployerCost),
		other("b3800000-0004-0000-0000-000000000205", "SS_ER_FP", "SS empresa formación profesional", KindEmployerCost),
		other("b3800000-0004-0000-0000-000000000206", "SS_ER_MEI", "SS empresa mecanismo de equidad intergeneracional", KindEmployerCost),
		// Information.
		other("b3800000-0004-0000-0000-000000000301", "INFO_BASE_CC", "Base de cotización por contingencias comunes", KindInformation),
		other("b3800000-0004-0000-0000-000000000302", "INFO_BASE_IRPF", "Base sujeta a retención", KindInformation),
	}
}
