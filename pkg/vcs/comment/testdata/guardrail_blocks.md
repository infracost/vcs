<h3>💰 Infracost report</h3>

<table>
  <tr><td colspan="2" width="1000px">Guardrails</td></tr>
  
<tr><td colspan="2" title="Blocking failure">
  
    <b>❌ Cost increase > $100</b>
  
</td></tr>
<tr><td></td><td>

Cost increase: 400.00/mo (400%) in project `my-project`
</td></tr>

  </table>
<details open>
  <summary><b>Monthly estimate increased by $400.00 📈</b> </summary>
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
      <td align="right">+$400.00</td>
      <td align="right">-</td>
      <td align="right">+$400.00 (+400%)</td>
      <td align="right">$500.00</td>
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

~ aws_instance.big
  +$400.00 ($100.00 → $500.00)

Monthly cost change for my-project
Amount:  +$400.00 ($100.00 → $500.00)
Percent: +400%

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

