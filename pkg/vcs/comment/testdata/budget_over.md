<h3>💰 Infracost report</h3>

This pull request is aligned with your company's FinOps policies and the Well-Architected Framework.
<hr/>

## Cost changes &amp; budgets

<table>
  <thead>
    <tr>
      <td align="left">Cost estimate</td>
      <td align="right">Previous&nbsp;cost</td>
      <td align="right">Cost&nbsp;change</td>
      <td align="right">New&nbsp;cost</td>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td align="left" valign="top">Repo `my-repo`</td>
      <td align="right" valign="top">$400</td>
      <td align="right" valign="top"><strong>$100</strong></td>
      <td align="right" valign="top">$500</td>
    </tr>
      <tr>
        <td colspan="4" align="left" valign="middle">Note: Repo cost estimates are based on baseline and usage costs configured within Infracost. See [our documentation](https://www.infracost.io/docs/features/usage_based_resources/) to learn more.</td>
      </tr>
  </tbody>
</table>
<table>
  <thead>
    <tr>
      <td align="left">Budget scope</td>
      <td align="right">Current cost</td>
      <td align="right">Budget</td>
    </tr>
  </thead>
  <tbody>
    <tr>
      <td align="left" valign="top">
        Tag `env: production`<br/>
        <sub>From Jan 2026 till Dec 2026</sub>
      </td>
      <td align="right" valign="top">$500</td>
      <td align="right" valign="top"><strong>🔴 $400 (OVER)</strong></td>
    </tr>
    <tr>
      <td colspan="3" align="left" valign="middle">🔴  Budget overrun detected</td>
    </tr>
    <tr>
      <td colspan="3" align="left" valign="middle">Note: Tag-based actual costs are calculated using service provider cost data for the current budget period for all resources tagged with `env`.</td>
    </tr>
  </tbody>
</table>
<details >
  <summary><b>Monthly estimate increased by $100 📈</b> </summary>
  <br/>

<table>
  <thead>
    <td>Changed project</td>
    <td><span title="Baseline costs are consistent charges for provisioned resources, like the hourly cost for a virtual machine, which stays constant no matter how much it is used. Infracost estimates these resources assuming they are used for the whole month (730 hours).">Baseline cost</span></td>
    <td><span title="Usage costs are charges based on actual usage, like the storage cost for an object storage bucket. Infracost estimates these resources using the monthly usage values in the usage-file.">Usage cost</span>*</td>
    <td>Total change</td>
    <td>New monthly cost</td>
  </thead>
  <tbody>
    <tr>
      <td>my-project</td>
      <td align="right">+$100</td>
      <td align="right">-</td>
      <td align="right">+$100 (+25%)</td>
      <td align="right">$500</td>
    </tr>
  </tbody>
</table>


*Usage costs can be estimated by updating Infracost Cloud settings, see [docs](https://www.infracost.io/docs/features/usage_based_resources/#infracost-usageyml) for other options.
  <details>
  <summary>Estimate details </summary>

```
Key: * usage cost, ~ changed, + added, - removed

──────────────────────────────────
Project: my-project

+ aws_instance.web
  Monthly cost depends on usage

Monthly cost change for my-project
Amount:  +$100 ($400 → $500)
Percent: +25%

──────────────────────────────────
Key: * usage cost, ~ changed, + added, - removed

*Usage costs can be estimated by updating Infracost Cloud settings, see [docs](https://www.infracost.io/docs/features/usage_based_resources/#infracost-usageyml) for other options.

No cloud resources were detected
```
  </details>

</details>

<hr/>

<sub>
  This comment will be updated when code changes.
</sub>

