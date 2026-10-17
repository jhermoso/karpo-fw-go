package domain

// WellKnownConcepts devuelve la siembra del catálogo de conceptos retributivos. Las deducciones
// conservan los GUID de las filas rrhh.deduction_type de C# (B3800000-0001-…); el resto son nuevos
// (B3800000-0004-…). El catálogo no lleva tipos: no hay motor de cálculo (véase docs/NOMINAS.md).
func WellKnownConcepts() []Concept {
	earning := func(id, code, name string, contributable, taxable bool) Concept {
		return Concept{ID: MustConceptID(id), Code: code, Name: name, Kind: KindEarning, Contributable: contributable, Taxable: taxable,
			PerceptionKey: "A", Active: true}
	}
	other := func(id, code, name string, kind ConceptKind) Concept {
		return Concept{ID: MustConceptID(id), Code: code, Name: name, Kind: kind, Active: true}
	}
	return []Concept{
		// Deducciones de la siembra de C# (mismos GUID).
		other("b3800000-0001-0000-0000-000000000001", "IRPF_GEN", "Retención IRPF general", KindIncomeTax),
		other("b3800000-0001-0000-0000-000000000002", "IRPF_PROF", "Retención IRPF actividades profesionales", KindIncomeTax),
		other("b3800000-0001-0000-0000-000000000003", "IRPF_AGR", "Retención IRPF actividades agrícolas", KindIncomeTax),
		other("b3800000-0001-0000-0000-000000000004", "IRPF_NR", "Retención IRNR no residentes", KindIncomeTax),
		other("b3800000-0001-0000-0000-000000000005", "SS_EMP_CG", "SS trabajador contingencias comunes", KindSocialSecurity),
		other("b3800000-0001-0000-0000-000000000006", "SS_EMP_DES", "SS trabajador desempleo", KindSocialSecurity),
		other("b3800000-0001-0000-0000-000000000007", "SS_EMP_FP", "SS trabajador formación profesional", KindSocialSecurity),
		other("b3800000-0001-0000-0000-000000000008", "EMBARGO", "Embargo judicial", KindDeduction),
		// Devengos.
		earning("b3800000-0004-0000-0000-000000000001", "SALARIO_BASE", "Salario base", true, true),
		earning("b3800000-0004-0000-0000-000000000002", "PLUS_CONVENIO", "Plus de convenio", true, true),
		earning("b3800000-0004-0000-0000-000000000003", "COMPLEMENTO", "Complemento personal", true, true),
		earning("b3800000-0004-0000-0000-000000000004", "HORAS_EXTRA", "Horas extraordinarias", true, true),
		earning("b3800000-0004-0000-0000-000000000005", "PAGA_EXTRA", "Paga extraordinaria", true, true),
		earning("b3800000-0004-0000-0000-000000000006", "PRORRATA_EXTRA", "Prorrata de pagas extraordinarias", true, true),
		earning("b3800000-0004-0000-0000-000000000007", "VACACIONES", "Vacaciones no disfrutadas", true, true),
		// Otras cotizaciones y deducciones del trabajador.
		other("b3800000-0004-0000-0000-000000000101", "SS_EMP_MEI", "SS trabajador mecanismo de equidad intergeneracional", KindSocialSecurity),
		other("b3800000-0004-0000-0000-000000000102", "SS_EMP_HEX", "SS trabajador horas extraordinarias", KindSocialSecurity),
		other("b3800000-0004-0000-0000-000000000103", "ANTICIPO", "Anticipo", KindDeduction),
		// Cotizaciones de la empresa (coste de empresa).
		other("b3800000-0004-0000-0000-000000000201", "SS_ER_CC", "SS empresa contingencias comunes", KindEmployerCost),
		other("b3800000-0004-0000-0000-000000000202", "SS_ER_ATEP", "SS empresa accidentes de trabajo y enfermedad profesional", KindEmployerCost),
		other("b3800000-0004-0000-0000-000000000203", "SS_ER_DES", "SS empresa desempleo", KindEmployerCost),
		other("b3800000-0004-0000-0000-000000000204", "SS_ER_FOGASA", "SS empresa FOGASA", KindEmployerCost),
		other("b3800000-0004-0000-0000-000000000205", "SS_ER_FP", "SS empresa formación profesional", KindEmployerCost),
		other("b3800000-0004-0000-0000-000000000206", "SS_ER_MEI", "SS empresa mecanismo de equidad intergeneracional", KindEmployerCost),
		// Información.
		other("b3800000-0004-0000-0000-000000000301", "INFO_BASE_CC", "Base de cotización por contingencias comunes", KindInformation),
		other("b3800000-0004-0000-0000-000000000302", "INFO_BASE_IRPF", "Base sujeta a retención", KindInformation),
	}
}
