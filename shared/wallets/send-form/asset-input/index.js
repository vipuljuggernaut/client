// @flow
import * as React from 'react'
import {Box2, Icon, NewInput, Text} from '../../../common-adapters'
import {collapseStyles, globalColors, styleSheetCreate} from '../../../styles'

type WarningTextProps = {|
  asset: string,
  payee?: string,
  warningType: 'overmax' | 'badAsset',
|}

export const WarningText = (props: WarningTextProps) => {
  const assetPrompt = (
    <Text type="BodySmallSemibold" style={{color: globalColors.red}}>
      {props.asset}
    </Text>
  )

  switch (props.warningType) {
    case 'overmax':
      return <Text type="BodySmallError">Your available to send is {assetPrompt}</Text>
    case 'badAsset':
      return (
        <React.Fragment>
          <Text type="BodySmallError">
            {props.payee} doesn't accept {assetPrompt}
          </Text>
          <Text type="BodySmallError">Please pick another asset.</Text>
        </React.Fragment>
      )
    default:
      break
  }
  return null
}

type Props = {
  bottomLabel: string,
  displayUnit: string,
  inputPlaceholder: string,
  onChangeAmount: string => void,
  onChangeDisplayUnit: () => void,
  onClickInfo: () => void,
  topLabel: string,
  warning?: React.Node,
}

const AssetInput = (props: Props) => (
  <Box2 direction="vertical" gap="xtiny" fullWidth={true} style={styles.flexStart}>
    {!!props.topLabel && (
      <Text type="BodySmallSemibold" style={collapseStyles([styles.topLabel, styles.labelMargin])}>
        {props.topLabel}
      </Text>
    )}
    <NewInput
      type="number"
      decoration={
        <React.Fragment>
          <Box2 direction="vertical" style={styles.flexEnd}>
            <Text type="HeaderBigExtrabold" style={styles.colorPurple2}>
              {props.displayUnit}
            </Text>
            <Text type="BodySmallPrimaryLink" onClick={props.onChangeDisplayUnit}>
              Change
            </Text>
          </Box2>
        </React.Fragment>
      }
      style={styles.colorPurple2}
      onChangeText={props.onChangeAmount}
      textType="HeaderBigExtrabold"
      placeholder={props.inputPlaceholder}
      placeholderColor={globalColors.purple2_40}
      error={!!props.warning}
    />
    {props.warning}
    <Box2 direction="horizontal" fullWidth={true} gap="xtiny">
      <Text type="BodySmall" style={styles.labelMargin}>
        {props.bottomLabel}
      </Text>
      <Icon
        type="iconfont-question-mark"
        color={globalColors.black_40}
        fontSize={12}
        onClick={props.onClickInfo}
      />
    </Box2>
  </Box2>
)

const styles = styleSheetCreate({
  colorPurple2: {color: globalColors.purple2},
  flexEnd: {
    alignItems: 'flex-end',
  },
  flexStart: {
    alignItems: 'flex-start',
  },
  labelMargin: {marginLeft: 1},
  text: {
    textAlign: 'center',
  },
  topLabel: {color: globalColors.blue},
})

export default AssetInput
