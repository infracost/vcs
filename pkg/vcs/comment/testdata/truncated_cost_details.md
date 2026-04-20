<h3>💰 Infracost report</h3>
This pull request is aligned with your company's FinOps policies and the Well-Architected Framework.
<details >
  <summary><b>Monthly estimate increased by $500 📈</b> </summary>
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
      <td>project-a</td>
      <td align="right">+$300</td>
      <td align="right">-</td>
      <td align="right">+$300 (+100%)</td>
      <td align="right">$600</td>
    </tr>
    <tr>
      <td>project-b</td>
      <td align="right">+$200</td>
      <td align="right">-</td>
      <td align="right">+$200 (+100%)</td>
      <td align="right">$400</td>
    </tr>
  </tbody>
</table>


*Usage costs can be estimated by updating Infracost Cloud settings, see [docs](https://www.infracost.io/docs/features/usage_based_resources/#infracost-usageyml) for other options.
  <details>
  <summary>Estimate details </summary>

```
Key: * usage cost, ~ changed, + added, - removed

──────────────�...infracost-usageyml) for other options.

5 cloud resources were detected:
∙ 5 were estimated
```
  </details>

</details>

<hr/>

<sub>
  This comment will be updated when code changes.
</sub>

