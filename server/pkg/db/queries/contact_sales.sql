-- name: CreateContactSalesInquiry :one
INSERT INTO contact_sales_inquiry (
    first_name,
    last_name,
    business_email,
    company_name,
    company_size,
    country_region,
    use_case,
    goals,
    consent_outreach,
    consent_updates,
    submitter_ip,
    user_agent
)
VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    sqlc.narg(submitter_ip)::inet,
    $11
)
RETURNING *;

-- name: CountRecentContactSalesByEmail :one
SELECT count(*) FROM contact_sales_inquiry
WHERE business_email = $1 AND created_at > now() - interval '1 hour';
