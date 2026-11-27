-- Employee seats are occupied by active employees, independently of API key lifecycle.
-- Preserve any higher configured capacity while lifting legacy key-based capacity enough
-- to keep every existing active employee usable.
UPDATE enterprises AS enterprise
SET seat_count = active_employees.employee_count
FROM (
    SELECT employee.enterprise_id, COUNT(*)::INTEGER AS employee_count
    FROM enterprise_employees AS employee
    WHERE employee.status = 'active'
    GROUP BY employee.enterprise_id
) AS active_employees
WHERE active_employees.enterprise_id = enterprise.id
  AND enterprise.seat_count < active_employees.employee_count;

-- dedicated_upstream_user_id is the enterprise's official upstream account. Keep its
-- concurrency aligned after the employee-based capacity correction above.
UPDATE users AS user_record
SET concurrency = enterprise.seat_count,
    updated_at = NOW()
FROM enterprises AS enterprise
WHERE enterprise.dedicated_upstream_user_id = user_record.id
  AND user_record.concurrency IS DISTINCT FROM enterprise.seat_count;
