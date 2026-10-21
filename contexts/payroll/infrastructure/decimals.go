package infrastructure

import "github.com/jhermoso/karpo-fw-go/pkg/persistence/sqlrepo"

// decimalColumnsV4 son las columnas que la migración 4 pasa de texto a {dec:p,s} (D-94). Siguen a
// employee_profile, employee_payroll_account, la cabecera de la nómina y payslip_line de Trench y
// Karpo C#; en payslip_line se toma el DDL, que allí discrepa de EF (EF dice (19,4) para
// unit_amount, base_amount y amount). El tipo de IRPF de la nómina es el del perfil. Tabla completa
// en docs/ESQUEMA-MIGRACIONES.md.
//
// La lista no se cambia nunca: el código de un Run no entra en el checksum de la migración, así que
// editarla haría divergir las bases nuevas de las ya migradas. Un cambio de (p,s) va en una
// migración nueva, con su propia lista, y se refleja en decimalColumns.
var decimalColumnsV4 = sqlrepo.DecimalColumns{
	{Table: "pay_profiles", Column: "income_tax_rate", Type: sqlrepo.Dec(5, 2), NotNull: true},
	{Table: "pay_profiles", Column: "salary_amount", Type: sqlrepo.Dec(18, 2)},
	{Table: "pay_profile_splits", Column: "split_percent", Type: sqlrepo.Dec(5, 2)},
	{Table: "pay_profile_splits", Column: "split_amount", Type: sqlrepo.Dec(18, 2)},
	{Table: "pay_payslips", Column: "income_tax_rate", Type: sqlrepo.Dec(5, 2)},
	{Table: "pay_payslips", Column: "gross", Type: sqlrepo.Dec(19, 4), NotNull: true},
	{Table: "pay_payslips", Column: "net", Type: sqlrepo.Dec(19, 4), NotNull: true},
	{Table: "pay_payslip_lines", Column: "quantity", Type: sqlrepo.Dec(18, 4)},
	{Table: "pay_payslip_lines", Column: "unit_amount", Type: sqlrepo.Dec(18, 4)},
	{Table: "pay_payslip_lines", Column: "base_amount", Type: sqlrepo.Dec(18, 4)},
	{Table: "pay_payslip_lines", Column: "rate_percent", Type: sqlrepo.Dec(5, 2)},
	{Table: "pay_payslip_lines", Column: "amount", Type: sqlrepo.Dec(18, 2), NotNull: true},
}

// decimalColumns son las columnas decimales vigentes del contexto, las que declaran los mapeos:
// hoy, las de la migración 4.
var decimalColumns = decimalColumnsV4
